<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/submission-assets/logo-lockup-dark.svg">
  <img alt="tela" src="docs/submission-assets/logo-lockup-light.svg" width="184">
</picture>

# tela

[![AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)
[![npm](https://img.shields.io/npm/v/tela-mcp?label=tela-mcp&color=7C3AED)](https://www.npmjs.com/package/tela-mcp)
[![MCP server](https://img.shields.io/badge/MCP-server-7C3AED)](mcp/README.md)

A self-hostable, markdown-native team wiki — spaces, nested pages, a Milkdown editor, ranked full-text + semantic search, live collaboration, comments, page history, public sharing, and a first-class MCP server so AI agents are first-class citizens alongside humans.

Public instance: **https://telawiki.com**

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/submission-assets/tela-page-dark.png">
  <img alt="tela page view" src="docs/submission-assets/tela-page-light.png" width="820">
</picture>

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

The Vite dev server proxies `/api` → backend `:8080`. `make dev` boots a local Postgres (`tela-dev-pg` on :55433) and points `TELA_DATABASE_URL` at it; the schema is migrated automatically on backend start (embedded migrations run by `db.Migrate()` — no separate migrate step). Open http://localhost:5173.

Run parts individually:

```bash
make be-dev     # backend only (go run ./cmd/tela)
make fe-dev     # frontend only (vite)
make storybook  # Storybook — the component dev surface (:6006)
```

## Production (Docker Compose)

```bash
make setup                           # write deploy/.env from the example with generated secrets
$EDITOR deploy/.env                  # set admin creds, public base URL, SMTP
make up                              # build + start the stack (Docker only — no host Node)
make logs                            # tail logs
make down                            # stop
```

The stack publishes a single host port **8780** (Caddy). `make setup` fills the
load-bearing secrets (`TELA_SHARE_SECRET`, `TELA_API_KEY_SECRET`,
`TELA_PG_PASSWORD`); set `TELA_PUBLIC_BASE_URL` and the `TELA_ADMIN_*` /
`TELA_SMTP_*` values yourself. If you leave the HMAC secrets unset entirely,
tela now generates and **persists** them on first boot (stable across restarts).

**Full setup, TLS, backups, and upgrades: see [`docs/self-hosting.md`](docs/self-hosting.md)** and the operations runbook [`docs/operations.md`](docs/operations.md). `deploy/.env.example` documents every variable.

## Make targets

```
make setup      # first run: write deploy/.env from the example with generated secrets
make dev        # backend + frontend in dev mode (parallel, no compose)
make be-dev     # backend only           make fe-dev     # frontend only
make storybook  # Storybook
make up         # build + start the compose stack on :8780 (stamps version/commit)
make down       # stop          make logs   # tail logs       make build  # rebuild images
make backup     # dump Postgres to ./backups   make restore FILE=...  # restore a dump
make clean FORCE=1            # stop AND delete volumes (destroys data)
make test-mcp-integration     # boot stack + run MCP↔backend E2E + tear down
make help
```

> Note: there is no `make lint` or `make migrate` — migrations run automatically on backend start. `make test` runs the backend suite (below).

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

tela was originally built by an autonomous agent system ("forge") and is now under conventional development. The old forge workspace (plan, run logs, task/idea history, research output under `docs/output/`) is archived out-of-tree — reference only, not part of this repo. This repo's code, `docs/`, and git history are the source of truth.

## License

tela is **open core**. Copyright © tela contributors. The **Community core — the whole product** — is licensed under the [GNU Affero General Public License v3.0](LICENSE) (AGPL-3.0): self-host, modify, and redistribute under its terms (run a modified version as a network service and you must offer your users the corresponding source). For a **commercial license** without AGPL obligations (e.g. to embed or offer tela as a closed service), contact the maintainer.

The **Enterprise Edition** (`backend/internal/ee/`, source-available, **not** AGPL) adds the company-of-record layer (SSO, audit, SCIM, governance) and requires a license key for production use — see [backend/internal/ee/LICENSE.md](backend/internal/ee/LICENSE.md). Full structure in [docs/licensing.md](docs/licensing.md) and [docs/editions-and-pricing.md](docs/editions-and-pricing.md).

**"tela", the tela name, and the tela logo are trademarks** and are **not** licensed under the AGPL — see [TRADEMARK.md](TRADEMARK.md). You may run and fork the code, but you may not use the tela branding for a redistributed or hosted version without permission.
