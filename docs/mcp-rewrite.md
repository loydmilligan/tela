# MCP Rewrite — One Server, In the Backend, Feature-Rich & Remote

Plan to collapse tela's MCP surface from the standalone TypeScript stdio package
(`mcp/`) into a single, remote, feature-rich MCP server hosted **inside the Go
backend**, speaking **Streamable HTTP**, built on the **official Go SDK**, and
reusing the existing PAT/session auth. Goal: less to maintain, and a server that
web AI hosts (Claude.ai, ChatGPT, the Anthropic Messages API connector) can
integrate richly with their own UX.

This doc is the contract for the rewrite. It supersedes the "add `/api/mcp`"
sketch. Research basis (current as of mid-2026) is summarized inline; the two
research reports that produced it are the source.

---

## 0. Decisions (locked)

| # | Decision | Choice | Why |
|---|---|---|---|
| D1 | Where it lives | **Go backend**, one implementation | The MCP surface *is* the REST surface; auth already lives here. Kills the two-codebase drift problem. |
| D2 | Transport | **Streamable HTTP** at `/api/mcp` (stateful sessions) | Current spec transport (replaced HTTP+SSE). Stateful so progress/elicitation/widgets work. Prod is single-instance, so no LB/session-store pressure yet. |
| D3 | SDK | **`github.com/modelcontextprotocol/go-sdk`** (GA, v1.x, pin latest) | Stable, no-breaking-changes guarantee, Google-backed; has output schemas, resources, prompts, completions, elicitation, notifications, StreamableHTTP handler. |
| D4 | In-process wiring | **Extract core funcs** — `xCore(ctx, *User, *APIKey, in) (out, *apiErr)`; HTTP route and MCP tool both call it | Ideal end-state, no in-process JSON round-trip, single source of truth per operation. Touches ~19 handlers. |
| D5 | Feature surface | **Full**: typed output schemas + annotations + resources(+links) + prompts + completions + pagination + progress + `list_changed`, **plus MCP Apps widgets** | This is the "let web AI hosts do nice stuff in their UX" payoff the rewrite is for. |
| D6 | Protocol revision | **2025-11-25** (latest; the SDK targets it) | — |

**D7 (Auth / Connect button) — settled in shape, AS provider TBD.** See §7.
The self-hostable artifact is **backend + frontend**; the polished MCP connector
(OAuth/Connect button) runs on the **canonical hosted instance** (tela.cagdas.io).
So: **PAT-bearer is the universal baseline** every backend ships — it works for
self-hosters and for the Messages API connector with zero OAuth. The **OAuth
Authorization Server is a config layer enabled on the hosted instance**
(delegate to WorkOS/Stytch — fine here, since self-hosters aren't forced to run
it), and the MCP host/AS is **configurable in MCP settings** so self-hosters who
want their own Connect button can point at their own AS later. Only the AS
*provider* for the hosted instance (WorkOS vs Stytch vs self-host Hydra) is still
open, and that decision isn't needed until Phase 5.

---

## 1. Target architecture

```
                       ┌───────────────────────────────────────────┐
  Claude.ai ───────────┤                                           │
  ChatGPT     OAuth 2.1 │   tela Go backend (single process)        │
  (Connect)   ──or PAT──┤                                           │
                        │   auth.Middleware (PAT | session)         │
  Messages API ─PAT────►│        │  injects *User + *APIKey + scope  │
  connector             │        ▼                                  │
                        │   POST/GET /api/mcp                        │
  Local clients ─PAT───►│   go-sdk StreamableHTTPHandler            │
  (http transport)      │        │                                  │
                        │        ▼                                  │
                        │   MCP tools / resources / prompts /       │
                        │   completions / widgets (api/mcp_*.go)    │
                        │        │ call                             │
                        │        ▼                                  │
                        │   xCore(ctx, user, apiKey, in) ──► DB     │
                        │        ▲                                  │
                        │   REST handlers (CreatePage, …) also call │
                        └───────────────────────────────────────────┘

  Optional bridge:  npx tela-mcp  ──stdio↔HTTP proxy──►  /api/mcp
                    (thin, no tool logic; deprecation shim only)
```

Key property: **`/api/mcp` is not a public path**, so `auth.Middleware` runs
first — the PAT/session is validated, `*auth.User` + `*auth.APIKey` (scope +
space pin) are in context, and method-level scope is pre-enforced — before the
MCP handler ever runs. Per-tool scope/space/role gating remains the tool's job,
exactly as REST handlers do it today.

---

## 2. The backend refactor (D4 — core extraction)

The REST handlers are tightly coupled to `http.ResponseWriter`/`*http.Request`:
they parse input from the HTTP layer, read auth from `r.Context()`, gate via
`w`/`r`-shaped helpers, and write JSON straight to `w` (no return value). To call
the logic in-process we split each into a transport-agnostic core.

### 2.1 Shared error type

Unify REST's `writeError` and MCP's error envelope. The `code` field is
load-bearing — agents react to specific codes (`api_key_scope`,
`api_key_space_scope`, `email_unverified`, …).

```go
// internal/api/apierr.go
type apiErr struct {
    Status  int    // HTTP status / MCP error mapping
    Code    string // stable machine code — load-bearing
    Message string
}
func (e *apiErr) Error() string { return e.Message }
```

REST wrapper maps `*apiErr` → `writeError(w, e.Status, e.Code, e.Message)`.
MCP tool maps `*apiErr` → `CallToolResult{IsError:true}` with
`{error, code, status}` JSON (same shape the TS client surfaces today, so agent
behavior keyed on `code` is preserved).

### 2.2 Per-operation core funcs

For every operation behind an MCP tool (§3 table), extract:

```go
func (s *Server) createPageCore(ctx context.Context, u *auth.User, k *auth.APIKey,
    in CreatePageIn) (Page, *apiErr) { /* validate, gate, DB, return row */ }

// HTTP route shrinks to: parse → core → writeJSON / writeError
func (s *Server) CreatePage(w http.ResponseWriter, r *http.Request) {
    in, err := decodeCreatePage(r); if err != nil { writeError(...); return }
    u := auth.UserFromContext(r.Context()); k, _ := auth.APIKeyFromContext(r.Context())
    out, ae := s.createPageCore(r.Context(), u, k, in)
    if ae != nil { writeError(w, ae.Status, ae.Code, ae.Message); return }
    writeJSON(w, 201, map[string]any{"page": out})
}
```

### 2.3 Auth helpers — ctx-returning twins

The role helpers already take ctx (`spaceRole(ctx, db, userID, spaceID)`,
`spaceRoleTx`) — reuse as-is. Only the two `w`/`r`-shaped gates need twins that
return `*apiErr` instead of writing to `w`:

- `enforceAPIKeySpaceScope(w,r,spaceID)` → `apiKeySpaceScopeErr(k, spaceID) *apiErr`
  (returns `api_key_space_scope` 403 or nil).
- `requireMembership(w,r,spaceID)` → `membership(ctx, db, userID, k, spaceID) (role, *apiErr)`.

The existing `w`/`r` gates become thin wrappers over the new ctx forms (call,
translate `*apiErr` to `writeError`), so REST behavior is unchanged.

### 2.4 Operations to extract (≈19)

`listSpaces, createSpace, getSpace, updateSpace, deleteSpace, importSpace,
importMira, listPages, createPage, getPage, updatePage, deletePage,
createComment, backlinks, createFeedback, search, searchBodies, ragSearch,
ragReadChunk`. (`/api/version` stays as-is — public, no core needed.)

Slug/URL building reuses `internal/api/slug.go` directly (the TS `slug.ts`
parity risk disappears).

### 2.5 Test impact

Each core func is now unit-testable without HTTP, against the existing
`testdb.New(t)` harness. Existing REST HTTP tests (`newWiredServer`,
`loginClient`) keep passing unchanged because the wrappers preserve behavior —
that's the regression guard for the extraction.

---

## 3. The MCP server module

Lives in package `api` (so it can call the unexported `xCore` methods directly,
no interface gymnastics): `api/mcp_server.go`, `api/mcp_tools.go`,
`api/mcp_resources.go`, `api/mcp_prompts.go`, `api/mcp_widgets.go`.

### 3.1 Mount + auth threading

```go
// router.go
mcpHandler := srv.newMCPHandler() // wraps mcp.NewStreamableHTTPHandler(...)
mux.Handle("/api/mcp", mcpHandler) // POST + GET; auto behind auth.Middleware
```

**Critical integration detail to nail in the Phase-1 spike:** the go-sdk tool
handlers receive their own `context.Context`. We must get `*auth.User` +
`*auth.APIKey` from the *HTTP request* context into the tool handler. Two
candidate paths against the SDK's `NewStreamableHTTPHandler(getServer, opts)`:
(a) the SDK propagates the request context to tool handlers — then
`auth.UserFromContext(ctx)` just works; or (b) build/select a per-session
`*mcp.Server` in the `getServer(r)` callback with the identity bound. Verify
which against the pinned SDK version before building tools — everything else
depends on it.

### 3.2 Tool registration pattern (typed in + out → output schema)

```go
type SearchIn  struct { Query string `json:"query" jsonschema:"search query"`
                        SpaceID *int64 `json:"space_id,omitempty"`
                        Cursor string `json:"cursor,omitempty"` }
type SearchOut struct { Results []SearchHit `json:"results"`; NextCursor string `json:"next_cursor,omitempty"` }

mcp.AddTool(server,
  &mcp.Tool{
    Name: "search", Description: "...",
    Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
  },
  func(ctx context.Context, req *mcp.CallToolRequest, in SearchIn) (*mcp.CallToolResult, SearchOut, error) {
      u := auth.UserFromContext(ctx); k, _ := auth.APIKeyFromContext(ctx)
      out, ae := s.searchCore(ctx, u, k, in)
      if ae != nil { return mcpErr(ae), SearchOut{}, nil }
      return nil, out, nil // SDK fills structuredContent + text from SearchOut
  })
```

Output schema is inferred from the `Out` struct → hosts get **structured
content** they can render/validate. Leave `Content` empty and the SDK
auto-fills text from the JSON.

### 3.3 Tool surface (port + enrich the 19)

Same 19 tools, but each gains a **typed output schema** and **annotations**:

| Annotation | Tools |
|---|---|
| `ReadOnlyHint` | `list_spaces, list_pages, get_page, search, search_bodies, semantic_search, read_chunk, list_backlinks` |
| `DestructiveHint` | `delete_page, delete_space` |
| `IdempotentHint` | `update_page, update_space` |
| (write, non-destructive) | `create_page, create_space, create_comment, import_markdown, import_mira, submit_feedback` |

`list_*` and `search*` gain **cursor pagination** (`cursor` in, `next_cursor`
out). `import_markdown` / `import_mira` / any reindex emit **progress
notifications**. The `submit_feedback` read-scope carve-out is preserved.

---

## 4. Feature surface (D5)

### 4.1 Resources (+ links)

- Keep the `tela://page/{id}` resource (preserves wikilink round-trip — tela
  writes `[Title](tela://page/{id})` into bodies). Register via
  `AddResourceTemplate`, `list: none` (spaces hold thousands of pages; rely on
  completion, not enumeration).
- Add `tela://space/{id}` and optionally `tela://space/{id}/page/{slug}`.
- **Resource links in tool results:** `search` / `semantic_search` / `get_page`
  / `create_page` return resource links (not just URL strings) so hosts render
  click-through chips.
- Emit `notifications/resources/list_changed` and `tools/list_changed` on
  relevant mutations (cheap, keeps hosts in sync). **Skip resource
  subscriptions** — low host support, high state cost.

### 4.2 Prompts

Server-provided prompt templates surfaced in the host's prompt picker. Ship a
small, high-value set for a wiki:

- `summarize-space` (args: space) — "summarize what's in this space"
- `draft-page` (args: space, topic) — scaffold a new page from a topic
- `release-notes` (args: space, since) — draft notes from recent pages
- `find-and-cite` (args: query) — semantic search + cite chunks

### 4.3 Completions

Argument autocomplete for `space_id` (space names), `parent_id`/`id` (page
titles within the resolved space), and `chunk_id`. Backed by the existing
list/search cores.

### 4.4 Elicitation (selective)

Use where it genuinely improves UX, not everywhere: disambiguate "which space?"
when a tool is called without `space_id` and the user belongs to several;
confirm before `delete_space` cascade. Requires stateful sessions (D2). Degrade
gracefully when a host doesn't support it (fall back to an error asking for the
arg).

### 4.5 Skip list (the "dumb stuff")

- **Sampling** — uneven host support; an LLM round-trip to the client mid-tool
  is fragile. If we want auto-summary, make it a tool that calls our own model
  server (the RAG embedder host), not client sampling.
- **Roots** — client filesystem capability; meaningless for a hosted wiki.
- **Resource subscriptions** — see §4.1.
- **Legacy HTTP+SSE transport** — deprecated; Streamable HTTP only.

---

## 5. MCP Apps widgets (D5 — the differentiator)

MCP Apps (official extension, 2026-01-26) lets a tool return an interactive
HTML/JS component rendered in a **sandboxed iframe** inside the host, with
bidirectional JSON-RPC over `postMessage`. It unifies OpenAI's Apps SDK and the
community MCP-UI standard. Both Claude (web+desktop) and ChatGPT are launch
supporters.

### 5.1 Mechanism

- Tool declares UI via `_meta` pointing at a `ui://…` **resource** that holds a
  bundled HTML/JS widget. Host renders the iframe; the widget reads the tool's
  `structuredContent` to draw itself.
- **ChatGPT specifics (Apps SDK):** resource `mimeType` = `text/html+skybridge`;
  tool links its widget via `_meta["openai/outputTemplate"]`; widget talks to
  ChatGPT via the `window.openai` bridge.
- **Claude:** same MCP Apps standard.
- ⚠️ The Go SDK has **no high-level widget helper** — we hand-assemble the
  `ui://` resource + `_meta`. And the exact Apps-SDK `_meta` key names must be
  **verified against live OpenAI docs before coding** (research couldn't load
  those pages; details came from secondary sources).

### 5.2 Widgets to build (start with 2–3)

1. **Rendered page preview** — `get_page` returns a widget showing
   rendered markdown (not raw), with a "open in tela" link.
2. **Search results cards** — `search`/`semantic_search` returns clickable
   result cards (title, snippet, space, citation) instead of a flat list.
3. **Diff / version view** — show a page revision diff.

### 5.3 Build approach

Widgets are **self-contained bundles** (sandboxed iframe — can't share the main
app's runtime). Build a small standalone bundle set (a dedicated Vite entry, or
a `widgets/` build), reusing tela's markdown renderer + tokens where it can be
bundled statically. Served by the backend as `ui://` resources (static asset
embed, like the migration files). Treat as a focused phase, not day-one.

---

## 6. The npm package fate (collapse)

`mcp/` stops holding tool logic. Options, recommended in order:

1. **Convert `tela-mcp` to a thin stdio↔HTTP proxy** (no tool knowledge — just
   forwards JSON-RPC to `/api/mcp` with the PAT). Keeps `npx tela-mcp` working
   for stdio-only hosts during deprecation. ~50 lines.
2. Point everyone at the HTTP transport directly
   (`claude mcp add --transport http tela https://tela.cagdas.io/api/mcp`); local
   Claude Code, Cursor, VS Code, etc. all speak HTTP now.
3. Delete the 19-tool TS implementation + its unit/smoke/integration suites
   (the Go protocol test replaces the drift guard — §8).

Net: one MCP implementation (Go), one optional ~50-line transport shim.

---

## 7. Auth (D7)

The self-hostable artifact is **backend + frontend**. The polished MCP connector
runs on the **canonical hosted instance** (tela.cagdas.io). This resolves the
self-hostability tension: the OAuth Connect experience is the hosted instance's
job, not a burden every self-hoster must carry. Two layers:

**Layer A — PAT-bearer (universal, ships in every backend).** The Streamable
HTTP server is authed by a pasted `tela_pat_…`, already wired end-to-end through
`auth.Middleware`. This works **today** with:
- the **Anthropic Messages API `mcp_servers` connector** (`authorization_token`
  = the PAT; beta header `anthropic-beta: mcp-client-2025-11-20`),
- MCP Inspector, and any local/remote client that accepts a bearer header.
Self-hosters get a fully working MCP server this way with **zero OAuth**. It does
not light up the Claude.ai/ChatGPT "Connect" button — that needs Layer B.

**Layer B — OAuth 2.1 (config layer, enabled on the hosted instance).** Phase 5.
The backend becomes an **OAuth 2.0 Resource Server**: implement in Go the parts
that are trivial and must be ours — **Protected Resource Metadata (RFC 9728)** at
`/.well-known/oauth-protected-resource` and **`WWW-Authenticate` on 401**. The
**Authorization Server is delegated** (WorkOS AuthKit or Stytch — DCR + CIMD +
Resource Indicators + PKCE for a config value, federated to tela login). Because
this is the hosted instance's concern, the SaaS dependency is acceptable.
- **Self-hoster seam:** the MCP host + AS endpoints are **configurable in MCP
  settings**, so a self-hoster who wants their own Connect button points at their
  own AS (their WorkOS/Stytch, or a self-run Hydra/Keycloak) — but is never
  *required* to; Layer A remains their no-config default.

Spec facts that shape Layer B: PRM + `WWW-Authenticate` are **MUST**; PKCE S256
is **MUST**; **DCR is only `MAY`** (CIMD is the `SHOULD` future) — not forced to
build dynamic client registration ourselves when the delegated AS provides it.
Do **not** hand-roll a full AS in fosite — that's the over-engineering trap;
delegate.

**Open (not needed until Phase 5):** which AS provider for the hosted instance —
WorkOS vs Stytch vs self-run Hydra.

---

## 8. Testing

- **Core funcs:** Go unit tests per `xCore` against `testdb.New(t)`.
- **REST regression:** existing HTTP tests unchanged — proves the wrapper split
  preserved behavior.
- **MCP protocol test (new drift guard):** wire a test server (`newWiredServer`),
  run the go-sdk in-process, drive it with the SDK *client* — `initialize`,
  `tools/list`, call every tool, assert structured output matches the typed
  schema. Replaces the TS `integration.live.test.ts` shape-drift guard.
- **Widget smoke:** render each widget bundle headless, assert it reads
  `structuredContent` and posts back over the bridge.
- **Auth:** test that `/api/mcp` 401s without a token, enforces scope
  (`api_key_scope` on a write tool with a read key), and space pinning
  (`api_key_space_scope`).

---

## 9. Phasing

| Phase | Deliverable | Usable by |
|---|---|---|
| **0. Spike** | Mount go-sdk StreamableHTTP at `/api/mcp`; nail auth-context threading (§3.1); one tool (`list_spaces`) end-to-end with PAT | Inspector |
| **1. Core extraction + tool port** | Extract ~19 `xCore` funcs (§2); port all tools with typed output schemas + annotations + pagination + progress | Messages API connector, local http clients (PAT) |
| **2. Resources + links + notifications** | `tela://` resources, resource links from search/get, `list_changed` | same |
| **3. Prompts + completions + elicitation** | prompt templates, arg autocomplete, selective elicitation | same |
| **4. npm collapse** | convert `tela-mcp` to stdio↔HTTP proxy; delete TS tool impl; docs | stdio-only hosts via proxy |
| **5. OAuth (D7)** | RS metadata + `WWW-Authenticate` + delegated AS (WorkOS/Stytch) on the hosted instance; host/AS configurable in MCP settings for self-hosters | **Claude.ai / ChatGPT Connect button** |
| **6. MCP Apps widgets** | 2–3 widgets (page preview, search cards, diff) | rich UX in Claude.ai / ChatGPT |

Phases 1–4 ship value with zero OAuth. Phase 5 unlocks consumer connectors.
Phase 6 is the UX differentiator and can run in parallel with 5.

### Phase 0 — DONE (spike landed)

Proven end-to-end with the official SDK (`go-sdk v1.6.1`), driving the real MCP
client over Streamable HTTP against the wired backend:

- **Mount:** `mux.Handle("/api/mcp", srv.MCPHandler())` (method-less → all verbs).
  `/api/mcp` added to `auth.IsPublicPath` so tela's Middleware skips it; the
  endpoint **self-authenticates**.
- **Auth seam (resolved — this was the linchpin):** the SDK threads a per-request
  `*auth.TokenInfo` from the request context into every tool call via
  `req.GetExtra().TokenInfo`. So the integration is a **`TokenVerifier` wrapping
  tela's `LookupAPIKey`** (`mcp.go` `mcpVerifier`), stashing the resolved tela
  `*User`+`*APIKey` in `TokenInfo.Extra`; tools read them back with
  `mcpIdentity(req)`. No per-session server rebuild; one shared server.
- **Gotcha found:** `auth.RequireBearerToken` **rejects a zero
  `TokenInfo.Expiration`** ("token missing expiration"). Set a rolling window
  (verifier re-runs per request; real PAT expiry stays enforced in
  `LookupAPIKey`).
- **Scope wrinkle (resolved):** a single POST transport carries read+write
  tools, so tela's method-level scope gate can't apply — `/api/mcp` being on
  `IsPublicPath` sidesteps it; per-tool scope enforcement is the tool's job
  (Phase 1).
- **Core-extraction pattern validated:** `listSpacesCore(ctx, *User) ([]…, *apiErr)`
  now backs both `GET /api/spaces` and the `list_spaces` tool; the existing REST
  integration test still passes (no behavior drift), and the tool returns typed
  **structured output with an inferred output schema**, correctly space-scoped.
- **Tests:** `TestMCP_SpikeListSpaces` (client→initialize→tools/list→call, asserts
  scoping + output schema) and `TestMCP_SpikeRejectsNoToken` both green.

This is the template every Phase-1 tool follows. Phase-5 OAuth slots into the
same `RequireBearerToken` seam via its `ResourceMetadataURL` option (PRM /
`WWW-Authenticate` already wired by the SDK).

---

## 10. Risks / things to verify before coding

1. ~~**SDK auth-context threading (§3.1)** — the linchpin; spike it in Phase 0.~~
   **RESOLVED in Phase 0** — `req.GetExtra().TokenInfo` + a `TokenVerifier`
   wrapping `LookupAPIKey`. See "Phase 0 — DONE".
2. ~~**Pinned go-sdk version**~~ — pinned to **v1.6.1** (latest stable); has
   StreamableHTTP, output-schema inference, the `auth` package with
   `RequireBearerToken` + PRM handler.
3. **Apps-SDK `_meta` key names** (`openai/outputTemplate`, `text/html+skybridge`,
   `window.openai`) — verify against live OpenAI docs before Phase 6.
4. **Stateful sessions in prod** — fine on single-instance archer; revisit
   (EventStore / session store) only if tela scales horizontally.
5. **ChatGPT write-connector gating** — full write connectors are Business/
   Enterprise/Edu only; Plus/Pro get read/fetch-only. Doesn't block us, but sets
   expectations for what individual ChatGPT users can do.
6. **Code-field parity** — agents key on `{code}`; the `apiErr` → MCP mapping
   must preserve every existing code string.

---

## 11. Reference implementations to crib from

- `getsentry/sentry-mcp` — production remote MCP for a real product API (full
  OAuth, stdio + remote).
- `cloudflare/workers-oauth-provider` — clean RFC 8414/9728/7591 map; read even
  if we delegate the AS.
- `coleam00/remote-mcp-server-with-auth` — Streamable HTTP + OAuth + Postgres
  end-to-end structure.
- Official Go SDK `examples/` + `auth`/`oauthex` packages — the Go-native
  starting point (no canonical Go remote+OAuth reference as polished as the TS
  ones yet).
