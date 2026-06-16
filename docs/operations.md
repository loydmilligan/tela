# Operating a tela instance

Day-2 runbook for a self-hosted tela. For first-time setup see
[`self-hosting.md`](self-hosting.md).

All commands assume the repo root and the Compose stack from `make up`.
`COMPOSE = docker compose -f deploy/docker-compose.yml`.

## Health & version

- `GET /api/health` → `{status, db}`, 200 when the DB is reachable, 503 otherwise.
- `GET /api/version` → `{version, commit, built_at}` (git metadata stamped at build).

```bash
curl -s localhost:8780/api/health
curl -s localhost:8780/api/version
```

The backend logs its effective config (`config: …`) at boot — base URL, cookie
`Secure`, SMTP mode, RAG target. Tail with `make logs`.

## Admin & users

The first admin is created one of two ways on an empty users table: from
`TELA_ADMIN_*` at boot when `TELA_ADMIN_PASSWORD` is set, or — when no admin env
is configured — through the web **setup wizard** at `/setup` (a fresh instance
redirects there automatically; the form creates a pre-verified instance admin
and signs in). After that, instance-admins manage users and plans in the app
under
**Settings → Users** (create users, reset passwords, toggle active/admin, assign
plan tiers) and configure the instance under **Settings** (admin tabs).

Instance settings (runtime config) are also available via the admin API:

```bash
# (authenticated as an instance admin)
curl -s localhost:8780/api/admin/settings
curl -s -X PATCH localhost:8780/api/admin/settings \
  -H 'content-type: application/json' \
  -d '{"settings":{"registration_open":"false"}}'
```

Secret values (HMAC keys, tokens) are never returned and can't be set this way —
they're managed via the environment / first-boot persistence.

### CLI subcommands

For headless operation (no app UI needed), the backend binary has ops
subcommands; run them in the container:

```bash
$COMPOSE exec backend /tela create-admin <username> <email> <password>  # make an instance admin
$COMPOSE exec backend /tela set-plan <user|org> <id> <plan_key>          # assign a plan tier
$COMPOSE exec backend /tela list-users                                   # id/username/email/admin/active/plan
$COMPOSE exec backend /tela reindex-all [--force]                        # re-embed (—force = full, ignore cache)
$COMPOSE exec backend /tela rag-eval --set golden.json                   # score retrieval (recall@k/MRR/nDCG)
```

### Recovering admin access

The env / wizard bootstrap only fires when the users table is empty. For an
existing instance where admin access is lost, mint a fresh admin directly:

```bash
$COMPOSE exec backend /tela create-admin recovery you@example.com 'a-strong-password'
```

## Semantic search reindex

After changing the embedder model (the chunk hash folds in the model name, so a
new model needs a full re-embed):

```bash
$COMPOSE exec backend /tela reindex-all
```

Runs synchronously to completion, logging per-space progress (and a `failed`
count — un-embeddable pages are skipped, not fatal). Requires `TELA_RAG_EMBED_URL`.

Add `--force` to bypass the per-chunk vector cache and re-embed **everything** —
the clean way to refresh when the model *name* is unchanged but the embedder
setup moved (replaces a manual `TRUNCATE page_chunks`). Normally not needed: the
index **self-heals** (failed reindexes retry with backoff; a background sweep
re-queues stale/unindexed pages and logs an `rag: index health` line each cycle),
so after an embedder outage the backlog clears on its own.

To measure retrieval quality against a golden set (see [`rag.md`](rag.md)):

```bash
$COMPOSE exec backend /tela rag-eval --set golden.json --k 10 --mode hybrid
```

## Backups

See [`self-hosting.md`](self-hosting.md#backups). `make backup` / `make restore
FILE=...`. Schedule `make backup` via cron for production.

## Resetting the database (destructive)

`make reset-prod-db FORCE=1` drops the Postgres volume and re-migrates from
scratch — **all data is lost**. Only for disposable/dev instances.

## Logs

```bash
make logs                              # all services, follow
$COMPOSE logs -f --tail=100 backend    # one service
```

Each request emits a structured access-log line (`http method=… path=… status=…
dur_ms=…` via `slog`; the `/api/health` probe is skipped). `/api/health` also
reports `rag: enabled|disabled`.

## Metrics

A Prometheus exposition is served at **`GET /metrics`** (`internal/api/metrics.go`).
It is **instance-admin gated** — a scraper authenticates with an admin-scoped PAT
(`Authorization: Bearer tela_pat_<key>`). Exported series include
`tela_http_requests_total{method,route,status}`,
`tela_http_request_duration_seconds{method,route}`,
`tela_client_errors_total{kind}` (see below), and the standard Go runtime +
process collectors.

## Client-side errors

Browser-side failures are beaconed to **`POST /api/client-errors`** and recorded
as `client.error` rows in the activity feed — visible in **Settings → Events**
under the **Errors** filter, with the full message + stack inline. Each report
bumps `tela_client_errors_total{kind}`, so a spike is alertable from `/metrics`.
The net spans both *uncaught* and *handled* failures, bucketed by `kind`:

- `error` / `unhandledrejection` — uncaught exceptions + rejected promises.
- `react` — a render-phase crash caught by the top-level `ErrorBoundary`.
- `query` / `mutation` — a failed data fetch/write, captured globally via
  TanStack Query's `QueryCache`/`MutationCache` (`lib/queryClient.ts`). Filtered
  to genuine breakage — network failures (status 0) and `5xx`; expected/handled
  cases (401 session-expiry, other 4xx, the rag/llm "disabled" 503 sentinels)
  are skipped so the feed stays signal.
- `resource` — a failed asset/chunk/image load.
- `collab` — reserved for live-collab failure reporting.

Reports are de-duped + capped per session client-side and rate-limited per user
server-side. The endpoint is authed (session/bearer); pre-login crashes on the
login screen are not captured. Frontend wiring: `frontend/src/lib/client-errors.ts`.

For triage, **Settings → Errors** (instance-admin) is a grouped "Issues" view:
client errors are collapsed by a server-computed fingerprint (kind + normalized
message + first stack frame, so ids/line-numbers don't fragment a group) into
one row per distinct error with a count, affected-user count, first/last seen,
and an expandable sample stack + recent occurrences. The raw chronological feed
stays under **Events → Errors**. Backend: `internal/api/admin_client_errors.go`
+ the `events.fingerprint` column (migration `0043`).

## Search-engine indexing (SEO)

Indexability is enforced **at the proxy** (`deploy/proxy/Caddyfile`), not the app
— the SPA can't set per-route response headers. The rule is *deny-by-default*:
every surface gets an `X-Robots-Tag: noindex` header **unless** it is meant for
the open web. Indexable surfaces:

- **The marketing landing** (`/`, `/mcp`, `/pricing`, `/privacy`, `/terms`) — any
  file that exists in the mounted `landing/dist`.
- **Public spaces & reader** (`/public/*`), **author/org homes** (`/u/*`), the
  **discovery directory** (`/discover`), and **handle pages** — `/{handle}` (a
  user or org home) and `/{handle}/{space-slug}` (a public space). These are the
  blog/profile surfaces, so they are served **without** the noindex header.
- The **public sitemap** (`/sitemap-public.xml`, backend-generated).

Everything else is noindex: the API (`/api/*`), private permalinks (`/p/*`),
share links (`/share/*`), and the whole auth-gated app shell (`/login`,
`/spaces/*`, `/settings`, `/search`, the editor, …). Crawler-UA hits on the
public surfaces are routed to the backend, which server-renders OG/JSON-LD
metadata (humans get the SPA, which sets its own per-page title/canonical).

> [!NOTE]
> A self-hosted instance is indexable the same way, but a **fresh** instance has
> no landing build and no public spaces — so crawlers see only noindex app routes
> until you publish a space or serve the landing. Keep your own instance private
> by simply not making any space public (the default).
