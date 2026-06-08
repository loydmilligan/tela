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
$COMPOSE exec backend /tela reindex-all                                  # re-embed after a model change
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

Runs synchronously to completion, logging per-space progress. Requires
`TELA_RAG_EMBED_URL` to be set.

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
reports `rag: enabled|disabled`. A Prometheus `/metrics` endpoint is still on the
roadmap.
