# CLAUDE.md — tela

Working context for tela. Read before contributing. Full architecture is in [`docs/`](docs/) and the deepest subsystem detail in [`docs/architecture.md`](docs/architecture.md); this file is the conventions + hard rules + the things that bite.

## What tela is

A self-hostable, markdown-native team wiki: Go + SQLite/FTS5 backend, React/TS frontend with a Milkdown editor, live Yjs collaboration, and a TypeScript MCP server so agents are first-class. Public face **https://tela.cagdas.io** (Cloudflare → host:8780). `pages.body` is canonical markdown forever — there is **no block table**.

> History: tela was built by an autonomous agent ("forge"), now under conventional development. The old forge workspace is archived at `archer:~/forgedata/tela/` (reference only — this repo + git history are the source of truth). Research/design docs from that era live under `archer:~/forgedata/tela/docs/output/` and were never committed here.

## Layout

- `backend/` — Go. Module `github.com/zcag/tela/backend`, entry `cmd/tela`. `internal/{api,auth,db,mdimport,miraimport,models}`. DB access is hand-written `database/sql` — **no sqlc, no ORM**. Migrations are embedded `NNNN_name.sql` files (forward-only, no down) in `internal/db/migrations`, run automatically on `db.Open()`.
- `frontend/` — React 19 + Vite + TS + Tailwind v4 + Radix + Milkdown (`@milkdown/kit`) + TanStack Query + TanStack Router + Orama + cmdk + Lucide + Storybook. `src/{components,lib,routes,styles}` + `App.tsx`/`main.tsx`. State is TanStack Query (zustand is in package.json but **unused**).
- `mcp/` — TypeScript MCP server, thin client over the REST API. Published as `tela-mcp` on npm. See `mcp/README.md`.
- `deploy/` — docker-compose + `proxy/Caddyfile`. `.env` is gitignored (narrow line, not `*.env`); `.env.example` is committed.

## Conventions

- **Issues:** every change ships behind a numbered issue. Commit format `type(scope): #NNN — summary` (e.g. `feat(backend): #142 — ...`). Concise messages, no co-author trailer.
- **Backend:** hand-written SQL via `database/sql`. New migration = new `NNNN_name.sql` (never edit an applied one; forward-only). One handler file per resource in `internal/api`.
- **Frontend hard rules (load-bearing):**
  1. Design tokens in `src/styles/tokens.css`, semantic names only. **Never** hardcode hex / raw px / ad-hoc radii.
  2. Theming via CSS custom properties on `[data-theme="..."]` — runtime switch, no rebuild.
  3. Radix + shadcn-style **owned** components only (`src/components/ui/`). No MUI/Chakra/Mantine/Ant/daisyUI.
  4. `@layer tokens, base, components, utilities` ordering is locked.
  5. Every new UI element uses owned primitives + tokens; missing primitive → build it (with a Storybook story) first.
  6. **Yjs is scoped:** imports allowed ONLY in `src/lib/collab/*` and the collab branch of `milkdown-editor.tsx`. Everything else explores pure-markdown / pure-SQL first.
- **MCP:** tools wrap REST endpoints; row-returning write tools wrap the row in a named envelope (`{ page: ... }`, `{ space: ... }`, `{ comment: ... }`, `{ feedback: ... }`); sentinel returns use `{ ok: true }`. Publish flow + hazards in `docs/architecture.md` (MCP section).

## Run / dev

```bash
make dev        # backend :8080 + frontend :5173 (vite proxies /api → :8080)
make storybook  # component dev surface
make up         # full docker stack on :8780 (prod-like; needs deploy/.env)
```

SQLite is created + migrated on first backend start (no migrate step). Backend config is env-driven (`TELA_PUBLIC_BASE_URL`, `TELA_SHARE_SECRET`, `TELA_API_KEY_SECRET`, `TELA_ADMIN_USERNAME/PASSWORD`, `TELA_MIRA_ALLOWED_HOSTS`); see `deploy/.env.example`.

## Tests

- Backend: `cd backend && go test ./...`. In-memory DB for non-concurrent; **on-disk** (`db.Open(filepath.Join(t.TempDir(),"tela.db"))`) for concurrency — `:memory:` is per-connection in modernc.org/sqlite. HTTP tests via `Handler(d)` + helpers `newWiredServer(t)`, `loginClient`, `newWiredServerOnDisk` (bearer-auth).
- MCP: `cd mcp && npm test` (mocked fetch) / `npm run test:smoke` / `test:integration` (needs live backend; `make test-mcp-integration` boots one). CI runs the integration suite.
- Frontend: **no test infra** (no jsdom/vitest). FE unit-test briefs bounce until a config is added.

## Gotchas (learned the hard way — full list in docs/architecture.md)

- **Commit ≠ deploy:** pushing does not deploy — `make up` does. Prod can silently keep running the old binary after a merge. After any backend deploy, `curl -s https://tela.cagdas.io/api/version` and compare `commit` to `git rev-parse --short HEAD`; if mismatch, `make up` then re-probe. Don't claim "live-verified" before this.
- **Secrets must be set and stable:** missing `TELA_API_KEY_SECRET`/`TELA_SHARE_SECRET` silently defaults to blank (compose only warns) → forgeable tokens. Rotating either invalidates outstanding PATs / share cookies. Diff `deploy/.env` against `.env.example` after any refresh.
- **Public-path bypass:** `auth.IsPublicPath` is a `HasPrefix` check over `/share/`, `/p/`, `/api/share/`, `/api/diagrams/`. Any new route under these prefixes bypasses session middleware — it MUST self-authenticate.
- **XFF / rate-limit:** Caddy is the only trusted upstream (`trusted_proxies static private_ranges`); backend reads the **rightmost** XFF hop, not leftmost.
- **Go 1.22+ ServeMux** rejects a literal segment after a wildcard (`/api/foo/{x}/literal`). Test new wildcard routes locally.
- **SQLite concurrency in tests** needs an on-disk DB (see Tests above).
- **mira fetch SSRF:** `TELA_MIRA_ALLOWED_HOSTS` is host-string only, **no IP-range guard** — never allowlist `localhost`/`127.0.0.1`/anything resolving to a private IP. Fetch is https-only and follows **no** redirects (`CheckRedirect: http.ErrUseLastResponse`) — never re-enable redirects.
- **FE public/share hooks use raw `fetch()`, not `api()`** — `api()` redirects to `/login` on 401, but in share-mode 401 means "password required".
- **Milkdown `SlashProvider` debounce wedges under React+Yjs** — don't drive `provider.update()` from a render effect; manage `dataset.show` + position manually (see architecture.md).
- **MCP release hazards:** verify `npm version` actually created the commit+tag; `bin` paths must not start with `./` (npm strips them); ESM entrypoint guard must `realpathSync` (npx symlinks). Details in architecture.md.

## Known drift

- `package.json` lists `zustand` but it is not imported anywhere — state is TanStack Query. Remove the dep or start using it.
