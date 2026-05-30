# tela — Architecture

## Overview

Three parts:

1. **Backend** (Go) — REST API over SQLite + FTS5; auth, collab WebSocket, business logic.
2. **Frontend** (React + TS) — SPA, Milkdown editor, talks to the backend over REST + a WebSocket for live collab.
3. **MCP server** (TypeScript) — exposes tela to AI agents over the Model Context Protocol; a thin bearer-authed client over the REST API.

```
┌──────────┐  REST + WS   ┌──────────┐   database/sql   ┌────────────────┐
│ Frontend │ ───────────▶ │ Backend  │ ───────────────▶ │ SQLite + FTS5  │
│ (React)  │              │  (Go)    │                  │ (tela-data vol)│
└──────────┘              └──────────┘                  └────────────────┘
                               ▲
                               │ REST (bearer PAT)
                          ┌──────────┐
                          │   MCP    │ ◀── AI agents (Claude Code, …)
                          │  (TS)    │
                          └──────────┘
```

`pages.body TEXT` is canonical markdown forever. There is **no block table** — the Milkdown editor reads/writes markdown, and Yjs is an overlay that rebases onto the markdown on save.

## Deploy topology

Docker Compose, three services on a private network; only Caddy publishes a host port (**8780**). `tela.cagdas.io` → Cloudflare → host:8780.

- **backend** — Go binary, internal `:8080`, SQLite on the `tela-data` volume.
- **frontend** — Vite static build served by nginx, internal `:80`.
- **proxy** — Caddy. Routes `/api/*` + `/p/*` + `/share/*` (UA bot-gate) + `/ws/*` → backend; everything else → frontend.

Backend image is distroless. Ad-hoc DB poke: `docker run --rm -v tela_tela-data:/data alpine:3 sh -c "apk add --no-cache sqlite && sqlite3 /data/tela.db '...'"`. `make up`/`make build` auto-stamp `TELA_VERSION` (`git describe --tags --always --dirty`) + `TELA_COMMIT` into the image, surfaced by `GET /api/version`.

## Backend layout (`backend/`)

- `cmd/tela/main.go` — entrypoint, wiring, graceful shutdown.
- `internal/api` — chi-free `net/http` ServeMux handlers, one file per resource (`spaces.go`, `pages.go`, `search.go`, `backlinks.go`, `comments.go`, `page_revisions.go`, `share_links.go`, `public_share*.go`, `api_keys.go`, `api_key_audit.go`, `og_image.go`, `page_diagrams.go`, `import.go`, `import_mira.go`, `feedback.go`, `version.go`, `health.go`, `pages_ws.go`, …) + `router.go` (`Handler(d *sql.DB) http.Handler`).
- `internal/db` — `db.go` (connection + helpers) and `migrate.go` (embedded forward-only runner). Migrations in `db/migrations/NNNN_name.sql`, applied on `Open()`, tracked in a `schema_migrations` table by filename.
- `internal/auth` — sessions (argon2id), bearer/API-key middleware, scopes, membership gate, public-path checks, audit log + GC.
- `internal/mdimport` — bulk markdown/zip import (named `mdimport`, not `import` — Go keyword).
- `internal/miraimport` — mira (mira.cagdas.io) single-page import: Tier-1/Tier-2/unknown block converters.
- `internal/models` — shared types.

DB access is hand-written SQL through `database/sql` on `modernc.org/sqlite`. **No sqlc, no ORM.** Go 1.22+ ServeMux pattern routing — note: a literal segment after a wildcard is rejected.

## Frontend layout (`frontend/src/`)

- `components/ui` — owned Radix+shadcn-style primitives (the only allowed component library).
- `components/app` — app-specific composed components.
- `lib` — non-component logic: `lib/collab/*` (Yjs transport — the only place Yjs may be imported), `lib/comments/*`, `lib/queries/*` (TanStack Query hooks), the Milkdown plugins (`milkdown-*.ts`), `milkdown-editor.tsx`.
- `routes` — TanStack Router route components.
- `styles` — `tokens.css` (semantic design tokens) + theme layers.

State is TanStack Query; routing is TanStack Router. The command palette (`AppCommandHost`) is a sibling of `RouterProvider`, so it uses `router.navigate()` not `useNavigate`.

## Data model

SQLite, built up by the embedded migrations (current set):

`0001_init` · `0002_search` (FTS5) · `0003_page_links` · `0004_auth` · `0005_yjs` · `0006_comments` · `0007_page_revisions` · `0008_share_links` · `0009_page_diagrams` · `0010_fts_strip_excalidraw` · `0011_api_keys` · `0012_api_key_audit` · `0013_feedback`.

Core shape: `spaces`, `pages` (`body TEXT` canonical markdown, soft-deleted), `space_members` (role: owner/editor/viewer), `pages_fts` (FTS5), page-link rows (backlinks from `[[wikilink]]` / `tela://page/{id}`), `users` + sessions, `comments`, `page_revisions`, `share_links`, `page_diagrams` (Excalidraw PNG sidecars), `api_keys` + audit, `feedback`.

- **Search:** SQLite FTS5. Body search builds a MATCH query in `buildFTSBodyMatch` (per-term quote-escape + `*` prefix wildcard); score `= -bm25/(1-bm25)`. The `0010` trigger strips Excalidraw fences from the FTS index.
- **Backlinks:** parsed from `[[wikilink]]` / `tela://page/{id}` on save.
- **Soft delete:** queries must filter out deleted rows.

## Request flow

Frontend → `/api/...` (Vite proxy in dev, Caddy in prod) → ServeMux → handler → `database/sql` → SQLite. MCP → the same `/api/...` with a bearer PAT.

## Subsystem notes (load-bearing)

### Auth & access
- Session cookie `tela_session` (HttpOnly, SameSite=Lax, `Secure` when `TELA_PUBLIC_BASE_URL` is https). argon2id passwords. Bootstrap admin via `TELA_ADMIN_USERNAME`/`_PASSWORD` (idempotent; unset password → generated + logged).
- **Bearer checked before cookie.** Invalid bearer does NOT fall back to cookie → explicit 401. Scopes `read`/`write`/`admin`; optional `space_id` pin → cross-space 403 `api_key_space_scope`. Path-aware carve-out lets `read` scope `POST /api/feedback` (`auth.scopeAllowsRequest`).
- **Membership gate** on `space_members.role`; no row → 403. Mutations require editor-or-owner. Instance-admin does NOT bypass page/space data.
- **Public paths** (`/share/`, `/p/`, `/api/share/`, `/api/diagrams/`) bypass session middleware via `IsPublicPath` (`HasPrefix`) — each MUST self-authenticate.

### Live collab
- `pages.body` is canonical; Yjs is an overlay that rebases on save. Custom WS transport (NOT y-websocket): 1-byte tag + big-endian payload — `0x01` update, `0x02` snap-req, `0x03` snap-resp, `0x04` sync-init, `0x05` awareness; unknown tags ignored; 16 MiB read limit; server pings 60s.
- FE shim `lib/collab/tela-provider.ts`. Reconnect 1s→30s; echo-skip via `origin === this`; disconnect removes awareness states before destroy; `pagehide` fires a removal frame.
- Leader election = lowest awareness `clientID`, gated at the editor's `markdownUpdated`/blur (not in `PageView.save()`). Cursors via `yCursorPlugin`, hue via `data-collab-color`.

### Comments
- **Anchored by text strings, never positions:** `{prefix, exact, suffix}` (~32-char window). Capture and resolve must share `view.state.doc.textBetween(0, doc.content.size, '\n')`. Flat/depth-1. Decoration plugin debounces 250ms (doc change) / 200ms (initial mount), mounts in both collab and non-collab branches.
- **Side-panel Sheet pattern (canonical):** `modal={false}` + `withOverlay={false}` + `onOpenAutoFocus` prevented + `onInteractOutside` guard. Used by CommentsPanel and ShareManagerSheet.

### History
- `page_revisions` snapshot in `UpdatePage` after commit, before the 200 write; only on body/title change; log-and-proceed. Soft-draft via `?draft=$revId` (owner-only); editor keyed `draft-${revId}` vs `live`.

### Body-fuzzy search
- Orama index per space, persisted to IndexedDB (`tela > bodyIndexes > space-<id>:v1`); palette tier-3. Logout sweeps via `clearAllBodyIndexes()`.

### OG / public share
- `/p/{id}`: UA-allowlist — bots get OG HTML, browsers 302 to `/pages/{id}`. `og_image.go` renders a 1200×630 PNG (package-level font, per-request face; no BiDi/RTL/emoji).
- Public share **reuses the FE in share-mode** at `/share/{token}` (route off root, no session). Token = 32B random base64url (43 chars), DB-unique. Cookie `tela_share_{token}` = `HMAC-SHA256(token||\0||page_id||\0||password_hash, TELA_SHARE_SECRET)`, constant-time compared. Rate limit: in-memory bucket per (token, rightmost-XFF IP), 5/min; identical 404 for missing/revoked/expired/bad-shape.

### Rich view
- Callouts (5 GitHub alert types), collapsibles (`<details>`), Excalidraw diagrams (sidecar `page_diagrams`, content-addressed PNG, `GET /api/diagrams/{page_id}/{file}` public+immutable, `PUT /api/pages/{id}/diagrams` editor+ 8 MiB PNG-magic-byte). Editor lazy-imports `@excalidraw/excalidraw`. Mermaid preserved as fenced code (not rendered).

### Markdown import
- `POST /api/spaces/{id}/import` (editor+), multipart `parent_id`/`dry_run`/`files`. Flatten-root pre-pass, parents-before-children, README-as-index, frontmatter→H1→filename title, `(2)`/`(3)` dedupe. FE Import tab uses raw `fetch()` (multipart), not `api()`.

### Mira import
- `POST /api/spaces/{id}/import-mira` (editor+), JSON `{parent_id?, source_url? | payload?}` (XOR enforced both layers). URL fetch: https-only host allowlist `TELA_MIRA_ALLOWED_HOSTS` (default `mira.cagdas.io`, empty = fail-closed), 5s / 1 MiB caps, Content-Type must be JSON, **no redirects** (`CheckRedirect: http.ErrUseLastResponse` — SSRF guard). Auto-appends `.json` to `/p/<slug>` URLs at ingress (`miraSlugPathRe`, allowlist-gated). Password-gated upstream (401 `password_required`) surfaces as 403 `mira_password_required` with an `unlock` URL (REST only; the MCP client currently strips the `unlock` field). Converters: `convert.go` (14 Tier-1 block types), `tier2.go` (15 placeholders), unknown → stub. Title = first `heading_1`. Two FE entry points: Settings → Import "From mira", and a Milkdown paste-hook popover.

### MCP
- `mcp/` subdir, ESM-only, Node ≥ 20, stdio transport. Thin client over REST with a bearer PAT; env `TELA_BASE_URL` + `TELA_API_KEY` required at spawn. 16 tools (read/write/space-CRUD/import/feedback). `tela://page/{id}` resource scheme matches the wikilink scheme. Startup fires an advisory `GET /api/version` compat check (never blocks; skips on non-semver like `dev`/SHAs). See `mcp/README.md` for the tool catalog.
- **SDK quirks:** subpath imports must end in `.js` even from TS; `registerTool` handler return needs an index signature (use `ok()`/`fail()` helpers); stdout must stay clean JSON-RPC (logs → stderr).
- **Release flow (headless, from `mcp/`):** `npm version patch --tag-version-prefix=tela-mcp-v && npm publish --access public`, then `git push --follow-tags` from repo root. **Hazards:** (1) verify `npm version` actually made the commit+tag (`git log -1` + `git tag --list 'tela-mcp-v*'`) — it can silently skip; (2) npm strips `bin` values starting with `./` — use `"tela-mcp": "dist/server.js"`; `npm publish --dry-run` first; (3) registry `@latest` has CDN lag — check `npm view tela-mcp versions --json`; (4) ESM entrypoint guard must `realpathSync(...)` both sides or npx-via-symlink exits 0 silently. Smoke via a temp symlink too.

## Decisions

See [`decisions.md`](decisions.md). For the full pitfall catalogue, see `CLAUDE.md` → Gotchas (this doc holds the subsystem detail behind them).
