<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/submission-assets/logo-lockup-dark.svg">
  <img alt="tela" src="docs/submission-assets/logo-lockup-light.svg" width="184">
</picture>

# tela

A self-hostable, markdown-native team wiki — spaces, nested pages, a Milkdown editor, full-text search, live collaboration, comments, page history, public sharing, and a first-class MCP server so AI agents are first-class citizens alongside humans.

Public instance: **https://tela.cagdas.io**

## Stack

| Part       | Tech                                                                                         | Location    |
|------------|---------------------------------------------------------------------------------------------|-------------|
| Backend    | Go + PostgreSQL (`pgx/v5` stdlib), `database/sql` (no ORM), embedded migrations              | `backend/`  |
| Frontend   | React 19 + TypeScript + Vite + Tailwind v4 + Radix + Milkdown + TanStack Query/Router + Orama | `frontend/` |
| Collab     | Yjs + y-prosemirror over a custom WebSocket transport (not y-websocket)                      | `frontend/` |
| MCP server | TypeScript (`@modelcontextprotocol/sdk`), published to npm as `tela-mcp`                     | `mcp/`      |
| Deploy     | Docker Compose — backend, frontend (nginx), Postgres, Caddy proxy; data on a Postgres volume | `deploy/`   |

`pages.body` is canonical markdown forever; there is no block table. The editor is Milkdown; Yjs is an overlay that rebases onto the markdown on save.

## Quickstart (local dev)

```bash
make dev        # backend (go run ./cmd/tela, :8080) + frontend (vite, :5173) in parallel
```

The Vite dev server proxies `/api` → backend `:8080`. `make dev` boots a local Postgres (`tela-dev-pg` on :55432) and points `TELA_DATABASE_URL` at it; the schema is migrated automatically on backend start (embedded migrations run by `db.Migrate()` — no separate migrate step). Open http://localhost:5173.

Run parts individually:

```bash
make be-dev     # backend only (go run ./cmd/tela)
make fe-dev     # frontend only (vite)
make storybook  # Storybook — the component dev surface (:6006)
```

## Production (Docker Compose)

```bash
cp deploy/.env.example deploy/.env   # fill in secrets — see below
make up                              # build + start the stack (auto-stamps git version/commit)
make logs                            # tail logs
make down                            # stop
```

The stack publishes a single host port **8780** (Caddy). Required secrets in `deploy/.env` (generate with `openssl rand -hex 32`, and **keep them stable across deploys** — rotating invalidates outstanding PATs / share cookies):

- `TELA_PUBLIC_BASE_URL` — e.g. `https://tela.cagdas.io`
- `TELA_SHARE_SECRET` — HMAC key for public-share password cookies
- `TELA_API_KEY_SECRET` — HMAC key for personal access tokens (PATs)

See `deploy/.env.example` for the full list.

## Make targets

```
make dev        # backend + frontend in dev mode (parallel, no compose)
make be-dev     # backend only           make fe-dev     # frontend only
make storybook  # Storybook
make up         # build + start the compose stack on :8780 (stamps version/commit)
make down       # stop          make logs   # tail logs       make build  # rebuild images
make clean FORCE=1            # stop AND delete volumes (destroys data)
make test-mcp-integration     # boot stack + run MCP↔backend E2E + tear down
make help
```

> Note: there is no `make test`, `make lint`, or `make migrate`. Tests are run per-component (below); migrations run automatically on backend start.

## Tests

```bash
make test                       # backend (boots a throwaway Postgres; per-test isolated DBs)
cd mcp && npm test              # MCP unit (mocked fetch) + npm run test:smoke / test:integration
```

The frontend has **no** unit-test harness today (no jsdom / vitest config). CI (`.github/workflows/ci.yml`) runs the MCP↔backend integration suite on changes to `backend/`, `mcp/`, `deploy/`, or the Makefile.

## Documentation

- [`docs/architecture.md`](docs/architecture.md) — system shape, repo layout, data model, and per-subsystem load-bearing details
- [`docs/decisions.md`](docs/decisions.md) — why PostgreSQL, custom collab transport, MCP-as-thin-client, etc.
- [`docs/api.md`](docs/api.md) — REST API surface
- [`CLAUDE.md`](CLAUDE.md) — conventions, hard rules, and the gotchas — **read before contributing**
- [`mcp/README.md`](mcp/README.md) — the MCP server (install, tool catalog, troubleshooting)

## History

tela was originally built by an autonomous agent system ("forge") and is now under conventional development. The old forge workspace (plan, run logs, task/idea history, research output under `docs/output/`) is archived at `archer:~/forgedata/tela/` — reference only, not part of this repo. This repo's code, `docs/`, and git history are the source of truth.

## License

TBD (the `mcp/` package is published MIT).
