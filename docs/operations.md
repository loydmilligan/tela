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

The first admin is bootstrapped from `TELA_ADMIN_*` when the users table is
empty. After that, instance-admins manage users and plans in the app under
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

### Recovering admin access

If you've lost the admin password and SMTP isn't configured for reset, set a new
known admin via env on a fresh boot only works when the users table is empty. For
an existing instance, reset the password hash directly:

```bash
$COMPOSE exec backend /tela --help   # (if a reset subcommand exists in your version)
# otherwise, as a last resort, update the password_hash in Postgres via psql.
```

> Promoting CLI parity (a `create-admin` / `set-plan` subcommand set) is on the
> hardening roadmap; until then, recovery is via SQL.

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

Logs are plain text on stdout (structured logging + a `/metrics` endpoint are on
the hardening roadmap).
