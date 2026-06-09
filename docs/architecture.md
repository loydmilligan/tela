# tela — Architecture

## Overview

Three parts:

1. **Backend** (Go) — REST API over PostgreSQL; auth, collab WebSocket, business logic.
2. **Frontend** (React + TS) — SPA, Milkdown editor, talks to the backend over REST + a WebSocket for live collab.
3. **MCP server** (TypeScript) — exposes tela to AI agents over the Model Context Protocol; a thin bearer-authed client over the REST API.

```
┌──────────┐  REST + WS   ┌──────────┐   database/sql   ┌────────────────┐
│ Frontend │ ───────────▶ │ Backend  │ ───────────────▶ │   PostgreSQL   │
│ (React)  │              │  (Go)    │   (pgx/v5 stdlib)│ (tela-pgdata)  │
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

Two deployment shapes share one codebase:

**(a) Standalone self-host** (`deploy/docker-compose.yml`, `make up`) — all-in-one. Only Caddy publishes a host port (**8780**); the `proxy` service is the edge (its `Caddyfile` declares the global block and `import`s `proxy/sites.caddy`).

- **postgres** — `pgvector/pgvector:pg17`, data on the `tela-pgdata` volume, `pg_isready` healthcheck. Internal only.
- **backend** — Go binary, internal `:8080`; reads `TELA_DATABASE_URL` (auto-built in compose from `TELA_PG_*`), `depends_on: postgres (service_healthy)`. `db.Open` retries the connection so it tolerates the gap before Postgres is query-ready.
- **gotenberg** — headless-Chromium HTML→PDF, internal only.
- **frontend** — Vite static build served by nginx, internal `:80`.
- **proxy** — Caddy. Routes `/api/*` + `/p/*` + `/share/*` (UA bot-gate) + `/ws/*` → backend; serves `landing/dist` at the apex; everything else → frontend.

**(b) Split** (`deploy/docker-compose.split.yml`) — for a host that already runs a **shared reverse proxy** (one Caddy per box owns `:443`). There is **no tela `proxy` container**: the external edge terminates TLS and `import`s the *same* `proxy/sites.caddy` (upstreams + landing root are env placeholders → it points them at the published loopback host ports). backend/frontend publish on **127.0.0.1:8781/8782**; **gotenberg** is a renderer reachable on your network (can be remote). Org **custom domains** work in this direct-TLS mode (on-demand cert issuance) but not in mode (a) behind an external terminator that owns TLS.

Backend image is distroless. Ad-hoc DB poke: `docker compose exec postgres psql -U tela -d tela -c '…'`. `make up`/`make build` auto-stamp `TELA_VERSION` (`git describe --tags --always --dirty`) + `TELA_COMMIT` into the image, surfaced by `GET /api/version`.

**Deploy.** Build-local + on-box registry: `make deploy` builds both images on the deploying machine, pushes only changed layers to a loopback `registry:2` on the box (`docker-compose.registry.yml`) over a transient SSH tunnel, syncs landing + `sites.caddy` to the dir the external edge serves static from, recreates the split stack from the just-pushed `:<commit>` tag, then `/api/version` health-gate. The box never builds — it only pulls. Partials: `deploy-backend|frontend|landing`. `make deploy-offline` is the no-registry fallback (`docker save | ssh docker load`). Move the registry off-box via `TELA_REGISTRY` + `REG_TUNNEL=0`. Configure `REMOTE`/paths in `deploy/deploy.env`. Full design in [`deploy.md`](deploy.md).

## Backend layout (`backend/`)

- `cmd/tela/main.go` — entrypoint, wiring, graceful shutdown.
- `internal/api` — chi-free `net/http` ServeMux handlers, one file per resource (`spaces.go`, `pages.go`, `search.go`, `backlinks.go`, `comments.go`, `page_revisions.go`, `share_links.go`, `public_share*.go`, `api_keys.go`, `api_key_audit.go`, `og_image.go`, `page_diagrams.go`, `import.go`, `import_mira.go`, `feedback.go`, `version.go`, `health.go`, `pages_ws.go`, …) + `router.go` (`Handler(d *sql.DB) http.Handler`).
- `internal/db` — `db.go` (Postgres connection pool via `pgx/v5` stdlib, with a ping-retry loop) and `migrate.go` (embedded forward-only runner). Migrations in `db/migrations/NNNN_name.sql`, applied by `Migrate()` on boot, tracked in a `schema_migrations` table by filename.
- `internal/auth` — sessions (argon2id), bearer/API-key middleware, scopes, membership gate, public-path checks, audit log + GC.
- `internal/mdimport` — bulk markdown/zip import (named `mdimport`, not `import` — Go keyword).
- `internal/miraimport` — mira (mira.cagdas.io) single-page import: Tier-1/Tier-2/unknown block converters.
- `internal/models` — shared types.
- `internal/testdb` — test helper: provisions a throwaway Postgres database per test (`New(t)` → CREATE DATABASE → migrate → drop on cleanup). Replaces the SQLite `:memory:`/on-disk pattern.

DB access is hand-written SQL through `database/sql` on the **`pgx/v5` stdlib** driver (positional `$1` placeholders; inserts return ids via `RETURNING`). **No sqlc, no ORM.** Go 1.22+ ServeMux pattern routing — note: a literal segment after a wildcard is rejected.

## Frontend layout (`frontend/src/`)

- `components/ui` — owned Radix+shadcn-style primitives (the only allowed component library).
- `components/app` — app-specific composed components.
- `lib` — non-component logic: `lib/collab/*` (Yjs transport — the only place Yjs may be imported), `lib/comments/*`, `lib/queries/*` (TanStack Query hooks), the Milkdown plugins (`milkdown-*.ts`), `milkdown-editor.tsx`.
- `routes` — TanStack Router route components.
- `styles` — `tokens.css` (semantic design tokens) + theme layers.

State is TanStack Query; routing is TanStack Router. The command palette (`AppCommandHost`) is a sibling of `RouterProvider`, so it uses `router.navigate()` not `useNavigate`.

## Data model

PostgreSQL. The SQLite migration history (`0001`–`0018`) was squashed into a single Postgres baseline `0001_init.sql` when the live DB held no data worth keeping; forward-only `NNNN_name.sql` from there.

Era-carried conventions (kept to minimize churn): datetimes are **TEXT** `'YYYY-MM-DD HH:MM:SS'` UTC (`DEFAULT tela_now()`, a SQL function; Go's `nowStamp()` emits the same), booleans are **INTEGER 0/1**, blobs are **BYTEA**, surrogate keys are **BIGINT IDENTITY** (inserts use `RETURNING id`). The org/group invariants are PL/pgSQL triggers (`RAISE EXCEPTION`); effective access is the `space_access` view (direct ∪ org ∪ group).

Core shape: `spaces`, `pages` (`body TEXT` canonical markdown), `space_members` (role: owner/editor/viewer), page-link rows (backlinks from `[[wikilink]]` / `tela://page/{id}`), `users` + sessions + `email_tokens`, `orgs`/`org_members`/`groups`/`group_members`/`space_grants`, `comments`, `page_revisions`, `share_links`, `page_diagrams` (Excalidraw PNG sidecars), `page_images`, `page_yjs_*`, `api_keys` + audit, `feedback`, `access_audit`.

- **Search:** ranked Postgres FTS. `/api/search` + `/api/search/bodies` run Postgres full-text over `pages.search_tsv` (title weighted above body, Excalidraw stripped in-SQL, ranked by `ts_rank_cd`, snippets via `ts_headline`, parsed with `websearch_to_tsquery`; migration `0004_pages_fts.sql`) — the old unranked `ILIKE` placeholder is gone. The semantic-enrichment tier (RAG over `pgvector`) is partially wired; the full two-tier instant + semantic design is in [`search.md`](search.md). tsvector / pg_trgm / pgvector all live in this one Postgres.
- **Backlinks:** parsed from `[[wikilink]]` / `tela://page/{id}` on save.
- **Soft delete:** queries must filter out deleted rows.

## Request flow

Frontend → `/api/...` (Vite proxy in dev, Caddy in prod) → ServeMux → handler → `database/sql` → PostgreSQL. MCP → the same `/api/...` with a bearer PAT.

## Subsystem notes (load-bearing)

### Auth & access
- Session cookie `tela_session` (HttpOnly, SameSite=Lax, `Secure` when `TELA_PUBLIC_BASE_URL` is https). argon2id passwords. First admin via `TELA_ADMIN_USERNAME`/`_PASSWORD`/`_EMAIL` (idempotent; only seeds when `_PASSWORD` is set; `_EMAIL` is pre-confirmed and also backfills an existing admin) OR — when no admin env is set — the web **setup wizard**: `GET /api/setup/status` reports `needs_setup` (users table empty), `POST /api/setup` atomically creates the first admin (insert guarded by `NOT EXISTS (SELECT 1 FROM users)`, else 409 `already_setup`) and signs in; the SPA `/setup` route mirrors the auth screens.
- **Email-first self-service** (`auth_register.go`, `internal/mailer`, migration `0015`): open `POST /api/auth/register` → confirm via emailed token (`verify-email`, which mints the session) → `Login` takes `identifier` (email **or** username). An account with a set-but-unconfirmed email is refused at login with `403 email_unverified`; `users.email` is nullable so legacy/bootstrap username-only rows skip the gate. Password reset = `request-password-reset` (always-202, no enumeration) + `reset-password`. `email_tokens` stores only the SHA-256 hash (verify TTL 24h, reset 1h, single-use, consumed in-tx); raw token rides the link. Email-sending endpoints are per-IP rate-limited (`authRateLimiter`, rightmost-XFF). Mailer is a provider-agnostic SMTP driver (go-mail) selected by `TELA_SMTP_*`; unset `TELA_SMTP_HOST` → `LogMailer` prints the link (dev/first-boot). Templates are branded inline-hex HTML (email clients can't do OKLCH/CSS-vars). FE routes: `/register`, `/verify-email`, `/forgot-password`, `/reset-password` (all off root, no app shell, share `AuthShell`).
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
