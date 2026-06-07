# CLAUDE.md — tela

Working context for tela. Read before contributing. Full architecture is in [`docs/`](docs/) and the deepest subsystem detail in [`docs/architecture.md`](docs/architecture.md); this file is the conventions + hard rules + the things that bite.

## What tela is

A self-hostable, markdown-native team wiki: Go + PostgreSQL backend, React/TS frontend with a Milkdown editor, live Yjs collaboration, and a TypeScript MCP server so agents are first-class. Public face **https://tela.cagdas.io** (Cloudflare → host:8780). `pages.body` is canonical markdown forever — there is **no block table**.

> History: tela was built by an autonomous agent ("forge"), now under conventional development. The old forge workspace is archived at `archer:~/forgedata/tela/` (reference only — this repo + git history are the source of truth). Research/design docs from that era live under `archer:~/forgedata/tela/docs/output/` and were never committed here.

## Layout

- `backend/` — Go. Module `github.com/zcag/tela/backend`, entry `cmd/tela`. `internal/{api,auth,db,mdimport,miraimport,models,testdb}`. **PostgreSQL** via the `pgx/v5` stdlib driver — DB access is hand-written `database/sql` (positional `$1` placeholders), **no sqlc, no ORM**. Migrations are embedded `NNNN_name.sql` files (forward-only, no down) in `internal/db/migrations`, run automatically by `db.Migrate()` on boot. `0001_init.sql` is a Postgres baseline squashed from the retired SQLite migration history (the live DB held no data worth keeping). Datetimes are TEXT in `'YYYY-MM-DD HH:MM:SS'` UTC (default `tela_now()`), booleans are INTEGER 0/1 — both kept from the SQLite era so Go scans + the frontend `parseSqliteTs` path are unchanged.
- `frontend/` — React 19 + Vite + TS + Tailwind v4 + Radix + Milkdown (`@milkdown/kit`) + TanStack Query + TanStack Router + Orama + cmdk + Lucide + Storybook. `src/{components,lib,routes,styles}` + `App.tsx`/`main.tsx`. State is TanStack Query (zustand is in package.json but **unused**).
- `mcp/` — **thin stdio↔HTTP proxy** to the backend's built-in MCP server (`/api/mcp`). As of v0.7 the tool/resource surface lives in the Go backend (`internal/api/mcp*.go`), NOT here — this package is a dumb pipe that forwards the MCP protocol over stdio with the PAT as a bearer header (so there's no second implementation to drift). Published as `tela-mcp` on npm. Modern hosts skip it and use HTTP transport directly. See `mcp/README.md` + `docs/mcp-rewrite.md`.
- `deploy/` — docker-compose + `proxy/Caddyfile`. `.env` is gitignored (narrow line, not `*.env`); `.env.example` is committed.
- `landing/` — standalone **marketing landing page** (Astro + Tailwind v4 + OKLCH tokens, self-hosted Geist). Separate static build from the app; `backend/`+`frontend/` are untouched. Locked contracts at repo root: `CONTENT.md` (copy), `DESIGN.md` (look), `ACCEPTANCE.md` (gates). Targets: `make landing-dev` / `landing-build` / `landing-gate`. Tokens in `landing/src/styles/tokens.css` are its own source of truth — never hardcode color/px (the token-conformance gate enforces it). See `landing/README.md`. Caddy serves `landing/dist/` at the apex `/` (the app keeps `/login`, `/spaces`, `/share/*`, `/api/*`); ship it with `make deploy-landing` (builds + recreates the proxy so it re-reads the static mount).

## Conventions

- **No issue/task tracker.** Do NOT open GitHub issues (or any other tickets) for this repo, ever. The `#NNN` references in older commits are artifacts from a retired system (forge) — do not continue or imitate them. Commit format is `type(scope): summary` (e.g. `feat(backend): hybrid chunk search`), no issue number. Concise messages, no co-author trailer.
- **Backend:** hand-written SQL via `database/sql`. New migration = new `NNNN_name.sql` (never edit an applied one; forward-only). One handler file per resource in `internal/api`.
- **Frontend hard rules (load-bearing):**
  1. Design tokens in `src/styles/tokens.css`, semantic names only. **Never** hardcode hex / raw px / ad-hoc radii.
  2. Theming via CSS custom properties on `[data-theme="..."]` — runtime switch, no rebuild.
  3. Radix + shadcn-style **owned** components only (`src/components/ui/`). No MUI/Chakra/Mantine/Ant/daisyUI.
  4. `@layer tokens, base, components, utilities` ordering is locked.
  5. Every new UI element uses owned primitives + tokens; missing primitive → build it (with a Storybook story) first.
  6. **Yjs is scoped:** imports allowed ONLY in `src/lib/collab/*` and the collab branch of `milkdown-editor.tsx`. Everything else explores pure-markdown / pure-SQL first.
- **MCP:** tools live in the backend (`internal/api/mcp_tools.go`) and call the same `xCore` funcs the REST routes do; row-returning tools wrap the row in a named envelope (`{ page: ... }`, `{ space: ... }`, `{ comment: ... }`, `{ feedback: ... }`) as typed output; sentinel returns use `{ ok: true }`. Per-tool scope via `mcpRequireWrite`; `/api/mcp` is on `IsPublicPath` and self-authenticates (bearer verifier). The npm `tela-mcp` proxy publish flow + hazards are in `docs/architecture.md` (MCP section).

## Run / dev

```bash
make dev        # backend :8080 + frontend :5173 (vite proxies /api → :8080); boots a local dev Postgres
make test       # backend tests against a throwaway Postgres (boots dev-db, see Tests)
make storybook  # component dev surface
make up         # full docker stack on :8780 (prod-like; needs deploy/.env)
```

The backend requires **Postgres** — `make dev` / `make be-dev` boot a local container (`tela-dev-pg` on :55433, via `make dev-db`) and point `TELA_DATABASE_URL` at it. The schema is created/migrated on boot by `db.Migrate()`. Backend config is env-driven: **`TELA_DATABASE_URL`** (`postgres://user:pass@host:5432/db?sslmode=disable`), `TELA_PUBLIC_BASE_URL`, `TELA_SHARE_SECRET`, `TELA_API_KEY_SECRET`, `TELA_ADMIN_USERNAME/PASSWORD/EMAIL`, `TELA_SMTP_*`, `TELA_MIRA_ALLOWED_HOSTS`. In the docker stack `TELA_DATABASE_URL` is auto-built from `TELA_PG_USER/PASSWORD/DB`; see `deploy/.env.example`.

**Auth is email-first** (`internal/api/auth_register.go`, `internal/mailer`): open self-registration (`POST /api/auth/register`) → email confirmation (`verify-email`, signs the user in) → login by **email or username** (`Login` accepts `identifier`; an account with an unconfirmed email is blocked with `403 email_unverified`). Password reset via `request-password-reset` / `reset-password` (always-202 on request, no enumeration). Email goes through a provider-agnostic SMTP `mailer.Mailer`; with `TELA_SMTP_HOST` unset it falls back to logging the link (dev/first-boot). `users.email` is nullable — legacy/bootstrap username-only rows skip the email gate. Tokens (`email_tokens`) are stored hashed; the raw token lives only in the link. Verify/reset emails are branded HTML (inline hex, not tokens — email clients can't do OKLCH/CSS-vars).

## Tests

- Backend: `make test` (boots the dev Postgres, then `go test ./...`). Or run `go test ./...` directly with `TELA_TEST_DATABASE_URL` set to a maintenance DSN (a reachable db like `postgres`). Each test gets its own throwaway database via `internal/testdb.New(t)` (CREATE DATABASE → migrate → drop on cleanup) — full isolation, and the old `:memory:`-is-per-connection hazard is gone (a pool against one real PG is shared across connections). HTTP tests via `Handler(d)` + helpers `newWiredServer(t)`, `loginClient`, `newWiredServerOnDisk` (the on-disk variants are now aliases — kept for callers).
- MCP: the backend MCP surface is tested in Go (`backend/internal/api/mcp_test.go`, run by `make test`). The `mcp/` proxy has one live E2E (`npm run test:integration`, needs a running backend; `make test-mcp-integration` boots one). CI runs the integration suite.
- Frontend: **no test infra** (no jsdom/vitest). FE unit-test briefs bounce until a config is added.

## Gotchas (learned the hard way — full list in docs/architecture.md)

- **Prod runs on `archer`** at `~/proj/tela` (deploy/.env lives there, not in dev checkouts). `tela.cagdas.io` → Cloudflare → archer:8780. Deploy **from any machine** with `make deploy` — it SSHes archer, `git pull --ff-only`, `make up`, then runs the health gate (polls `/api/version`, compares `commit` to local HEAD, fails loudly on mismatch). Per-component: `make deploy-backend` / `deploy-frontend` / `deploy-landing` (recreate one service); `make reset-prod-db FORCE=1` wipes the Postgres volume.
- **Commit ≠ deploy:** pushing does not deploy — `make deploy` does (it rebuilds on archer). The built-in health gate now catches a stale binary automatically; if you deploy by hand, still `curl -s https://tela.cagdas.io/api/version` and compare `commit` to `git rev-parse --short HEAD` before claiming "live-verified".
- **Secrets must be set and stable:** missing `TELA_API_KEY_SECRET`/`TELA_SHARE_SECRET` silently defaults to blank (compose only warns) → forgeable tokens. `TELA_PG_PASSWORD` has **no default** (compose fails without it). Rotating the secrets invalidates outstanding PATs / share cookies. Diff `deploy/.env` against `.env.example` after any refresh.
- **Public-path bypass:** `auth.IsPublicPath` is a `HasPrefix` check over `/share/`, `/p/`, `/api/share/`, `/api/public/`, `/api/diagrams/`, and `/dav/` (WebDAV sync — self-authenticates via PAT-as-Basic, scope-gated per verb in `DAVHandler`; see `docs/webdav-sync.md`). Any new route under these prefixes bypasses session middleware — it MUST self-authenticate.
- **Public spaces (blog surface):** `spaces.visibility` (`private`|`public`, migration `0012`) makes a *whole* space readable with no login. Public read-only API under `/api/public/` (`public_spaces.go`, `public_users.go`) self-authenticates on `visibility='public'`; it's GET-only and **publishing grants no write** (adds no `space_access`) — keep it that way. SPA routes `/public/spaces/{id}` (front-page index) + `/public/spaces/{id}/pages/{id}/{slug}` (reader) + `/u/{handle}` (user home) are unauthenticated children of `rootRoute`; `/p/{id}` redirects public-space pages to the reader. Full design + the SEO follow-up are in `docs/public-spaces.md`. Owner-only flip via `PATCH /api/spaces/{id}` `{visibility}`.
- **XFF / rate-limit:** Caddy is the only trusted upstream (`trusted_proxies static private_ranges`); backend reads the **rightmost** XFF hop, not leftmost.
- **Go 1.22+ ServeMux** rejects a literal segment after a wildcard (`/api/foo/{x}/literal`). Test new wildcard routes locally.
- **pgx is strict about types:** a SQL `boolean` expression scanned into a Go `int` errors (SQLite silently returned 0/1). Return `CASE WHEN … THEN 1 ELSE 0 END` for INTEGER-boolean columns, or scan into `bool`. Same for any implicit text↔int comparison SQLite tolerated.
- **Search is ranked Postgres FTS:** `/api/search` + `/api/search/bodies` run ranked Postgres full-text over `pages.search_tsv` (`ts_rank_cd`, `ts_headline`, `websearch_to_tsquery`; migration `0004_pages_fts.sql`) — the old unranked `ILIKE` placeholder is **gone**. The semantic-enrichment tier (RAG over `pgvector`) is partially wired; the full two-tier instant+semantic design is in `docs/search.md`.
- **RAG embedder is a remote Ollama (`TELA_RAG_EMBED_URL`):** the `/api/rag/*` + MCP `semantic_search` path embeds via an external Ollama; with the var unset the feature no-ops (503), so it's an operational dependency, not in-process. In **prod it points at `http://tardis:11435`** — a *dedicated embed-only* Ollama instance (model **`qwen3-embedding:0.6b`**, 1024-d — was `mxbai-embed-large`; both 1024-d so the `page_chunks.embedding vector(1024)` column is unchanged; `keep_alive=-1`, isolated from the agent-model churn on tardis `:11434` so it stays warm). The model is set via `TELA_RAG_EMBED_MODEL` in archer's `deploy/.env` (code default is still `mxbai-embed-large`). If RAG/semantic search starts 503ing or hanging, check that instance is up (`curl tardis:11435/api/ps`). Setup lives in dotty `common/infra/ai/ollama/` (`make -C ~/dotty/common/infra ollama`). **Changing the model = re-embed everything:** the chunk hash folds in the model name, so after editing `TELA_RAG_EMBED_MODEL` + `make deploy-backend`, run `docker compose -f deploy/docker-compose.yml exec backend /tela reindex-all` on archer (the `reindex-all` subcommand re-embeds every space; resumable).
- **mira fetch SSRF:** `TELA_MIRA_ALLOWED_HOSTS` is host-string only, **no IP-range guard** — never allowlist `localhost`/`127.0.0.1`/anything resolving to a private IP. Fetch is https-only and follows **no** redirects (`CheckRedirect: http.ErrUseLastResponse`) — never re-enable redirects.
- **FE public/share hooks use raw `fetch()`, not `api()`** — `api()` redirects to `/login` on 401, but in share-mode 401 means "password required".
- **Milkdown `SlashProvider` debounce wedges under React+Yjs** — don't drive `provider.update()` from a render effect; manage `dataset.show` + position manually (see architecture.md).
- **MCP release hazards:** verify `npm version` actually created the commit+tag; `bin` paths must not start with `./` (npm strips them); ESM entrypoint guard must `realpathSync` (npx symlinks). Details in architecture.md.

## Known drift

- `package.json` lists `zustand` but it is not imported anywhere — state is TanStack Query. Remove the dep or start using it.
