# M7.0 Transport Validation — WebSocket through Cloudflare → Caddy → Backend

**Date:** 2026-05-19  
**Task:** #61  
**Verdict: WebSocket works end-to-end through the full production stack.**

---

## Final Verdict

WebSocket connections establish and exchange data cleanly through both test paths:

| Path | Result |
|---|---|
| `ws://localhost:8780/ws/echo` (direct Caddy) | **PASS** — 101 + echo round-trip |
| `wss://tela.cagdas.io/ws/echo` (Cloudflare → Caddy → backend) | **PASS** — 101 + echo round-trip |

No additional Cloudflare configuration was needed. No Caddy directives beyond the path matcher were needed.

---

## Method

A throwaway Go ws echo handler was added to the backend (stdlib only — no external deps, removed before commit). It:
1. Validates the `Upgrade: websocket` header and `Sec-WebSocket-Key`
2. Hijacks the connection via `http.Hijacker`
3. Sends a proper 101 response with `Sec-WebSocket-Accept` computed per RFC 6455
4. Echoes text frames, responds to pings, closes cleanly on close frames

A Python stdlib test client (no external packages) performed the full handshake + text echo + close sequence against both endpoints.

---

## Caddyfile Change Required

The existing Caddyfile only had `/api/*` and a catch-all (`handle {}`) pointing to the frontend. `/ws/*` paths fell to the frontend and returned 200 HTML — the backend never saw them.

**Fix:** Add a `handle /ws/*` block before the catch-all:

```caddyfile
:80 {
    handle /api/* {
        reverse_proxy backend:8080
    }

    handle /ws/* {
        reverse_proxy backend:8080
    }

    handle {
        reverse_proxy frontend:80
    }
}
```

Caddy's `reverse_proxy` directive handles WebSocket upgrades transparently by default — it passes the `Upgrade: websocket` and `Connection: Upgrade` headers through and does full-duplex TCP tunneling. No additional `header_up`, `transport`, or `flush_interval` directives are needed.

The updated Caddyfile is committed at `deploy/proxy/Caddyfile`.

---

## Auth Middleware Behavior for WebSocket Upgrades

**Cookie auth works cleanly on ws upgrade requests.** The full sequence:

1. Browser/client sends HTTP GET with `Upgrade: websocket` header and `tela_session` cookie.
2. `auth.Middleware` runs first (it wraps the entire mux). It reads the cookie, calls `LoadSessionAndSlide`, and — if valid — calls `next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))`.
3. The ws handler inside `next` calls `w.(http.Hijacker).Hijack()`, taking ownership of the underlying TCP connection. The 101 response is sent directly over the hijacked bufio.Writer.
4. `auth.Middleware` returns normally after `next.ServeHTTP` returns (which happens when the handler goroutine exits). The middleware writes nothing after the handler returns — there is no response-writer wrapping or response-code logging in the middleware that could conflict with hijacking.

**Verified behaviors:**
- No valid session → middleware writes `{"error":"unauthorized","code":"unauthorized"}` with HTTP 401 and the ws upgrade never happens. Correct.
- Valid session → 101 upgrade completes, full-duplex ws established, user available via `auth.UserFromContext(r.Context())` inside the handler.

**No bypass needed.** The `/ws/pages/{id}` route must NOT be added to `IsPublicPath` — cookie auth works exactly as-is on the upgrade request.

**One caveat:** Auth middleware slides (extends) the session on every authenticated request, which involves a DB write. For long-lived ws connections this slide happens once on connect (not per-frame). That is the correct and desired behavior.

---

## Cloudflare Behavior

Cloudflare (free plan, orange-cloud proxy) passes WebSocket upgrades transparently on port 443 (HTTPS).

Response headers from the CF test:
```
cf-cache-status: DYNAMIC
Server: cloudflare
CF-RAY: 9fe15ad23ecf4d9e-FRA
```

- `cf-cache-status: DYNAMIC` — Cloudflare correctly identifies ws as non-cacheable dynamic traffic.
- No special CF rules, Workers, or Page Rules needed.
- The free plan does support WebSocket proxying by default (this was restricted on very old free plans but has not been the case since ~2019).

---

## Recommended Path Pattern for Real Yjs Endpoint

**Use `/ws/pages/{id}` as planned.** Findings:
- The Caddyfile `handle /ws/*` block covers this pattern.
- Go 1.22+ mux registration: `mux.HandleFunc("GET /ws/pages/{id}", srv.WsPages)` — the `{id}` wildcard works as expected.
- The `/ws/` prefix cleanly separates ws routes from REST routes (`/api/`). No ambiguity.

No amendments to the plan needed.

---

## WebSocket Library Recommendation for #62

**Recommend: `github.com/coder/websocket` v1.8.x**

### Comparison

| | `gorilla/websocket` v1.5.3 | `github.com/coder/websocket` v1.8.13 |
|---|---|---|
| Last release | Dec 2023 | Active (maintained by Coder) |
| Context-aware I/O | No (set deadlines manually) | Yes (`Read(ctx, ...)`, `Write(ctx, ...)`) |
| Concurrent writes | Serialize manually | Safe via `conn.Write(ctx, ...)` |
| HTTP/2 ws (RFC 8441) | No | Yes |
| Wasm support | No | Yes (future-proof) |
| API style | Upgrader + `*Conn` methods | `Accept()` + `*Conn` with context |
| Battle-tested | Extremely (ubiquitous) | Moderate (Coder uses in production) |
| Module stability | Stable, no breaking changes | Module path changed nhooyr→coder once |

### Why `coder/websocket` for #62

The #62 relay pattern involves:
- Multiple concurrent client goroutines per page room, each reading from their ws connection
- A fan-out broadcast: one client's write → all other clients on the same page
- Clean teardown: when a client disconnects, cancel their goroutine and remove from room

`coder/websocket`'s context-aware reads and writes make this cleaner:
```go
// Graceful teardown: cancel ctx → conn.Read returns immediately
msgType, data, err := conn.Read(ctx)
```

With gorilla, the equivalent requires `conn.SetReadDeadline(time.Now())` in a separate goroutine to unblock a stuck read — boilerplate that `coder/websocket` eliminates.

### Gorilla is fine too

If the team is more comfortable with gorilla, it's a valid choice. The relay+persister pattern is achievable with gorilla — you just manage lifecycle with goroutine-level `done` channels and `SetDeadline` calls. The difference is ergonomic, not correctional.

### Import path to use

```go
import "github.com/coder/websocket"
```

Add with: `go get github.com/coder/websocket@latest` (currently v1.8.13+). The old `nhooyr.io/websocket` path is a redirect and still resolves, but use the canonical `coder` path in new code.

---

## Gotchas for #62

1. **Response-writer hijacking after middleware** — no issue (verified above). The ws handler gets a clean ResponseWriter it can hijack.
2. **Path matcher order in mux** — `handle /ws/*` must appear before the catch-all in Caddyfile. Already done in the committed Caddyfile.
3. **Caddy restart vs reload on Caddyfile change** — `caddy reload` does a hot reload but Docker bind mounts with the Write tool change the inode (atomic replace). `caddy reload` reads the old inode; a container restart is required to pick up the new file. Note this for CI/deploy scripts.
4. **Y.Doc lifecycle** — create `Y.Doc` in a stable `useRef` with lazy init (documented in memory.md). Avoid creating it in render.
5. **Cloudflare idle timeout** — Cloudflare's proxy has a 100-second idle timeout on WebSocket connections. If no frames flow for >100s, CF will close the connection. The client needs to send periodic pings or the server needs to send pings every ~60s. Implement a ping loop in the #62 ws relay.
6. **Frame size** — Yjs updates can be large (full state vector on first sync). Test with binary frames >64KB to ensure the relay handles 16-bit extended payload length correctly.
