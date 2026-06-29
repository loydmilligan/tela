# tela first-responder runbook

Ops reference for telawiki.com (split topology). Developer/infra audience.  
For day-2 admin tasks (user management, reindex, backups) see [`operations.md`](operations.md).  
For deployment mechanics see [`deploy.md`](deploy.md).

---

## Key URLs

| URL | Purpose |
|---|---|
| `https://telawiki.com/api/health` | Liveness — 200 OK or 503 degraded |
| `https://telawiki.com/api/version` | `{version, commit, built_at}` — verify what's live |
| `https://telawiki.com/metrics` | Prometheus scrape (admin PAT required as `Authorization: Bearer tela_pat_…`) |

---

## Service map

Production runs as the **split topology** (`deploy/docker-compose.split.yml`).  
No tela-owned proxy container — the **shared external Caddy** on the host owns `:443` and imports `deploy/proxy/sites.caddy`.

| Service | Image | Published port | Role |
|---|---|---|---|
| `backend` | `tela-backend` | `127.0.0.1:8781` → `:8080` | Go API, migrations, MCP, WebDAV, collab WS |
| `frontend` | `tela-frontend` | `127.0.0.1:8782` → `:80` | nginx serving the React SPA |
| `postgres` | `pgvector/pgvector:pg17` | internal only | all wiki data; volume `tela-pgdata` |
| `deck` | `tela-deck` | internal `:3344` | Slidev sidecar — renders slide images/PDF/SPA |
| `umami` | `ghcr.io/umami-software/umami` | `127.0.0.1:3001` | landing page analytics |
| `umami_db` | `postgres:16-alpine` | internal only | Umami's own Postgres; volume `umami_db_data` |
| **external Caddy** | host process | `:443`, `:80` | TLS termination, routing, on-demand org certs |
| **Ollama embedder** | external host | configured in `TELA_RAG_EMBED_URL` | semantic search / RAG (optional) |

**Dependency order:** `postgres` must be healthy before `backend` starts (`depends_on: postgres: condition: service_healthy`). `deck` must be started (not necessarily healthy) before `backend`.

---

## Triage flow

```
alert fires
    └─ curl -s https://telawiki.com/api/health | jq
          ├─ {"status":"ok","db":"ok","rag":"enabled|disabled"}
          │    └─ backend + DB healthy; check app-level errors (5xx rate, client errors)
          └─ {"status":"degraded","db":"error: …"}    HTTP 503
               ├─ backend is up but can't reach Postgres → see "DB down" below
               └─ no response at all → backend container is down → check logs
```

Check what commit is live:
```bash
curl -s https://telawiki.com/api/version | jq
```

---

## Logs

All commands run on the deploy host in `REMOTE_DIR` (the repo checkout, default `~/tela`).

```bash
# Tail all services
docker compose -f deploy/docker-compose.split.yml logs -f --tail=100

# One service
docker compose -f deploy/docker-compose.split.yml logs -f --tail=200 backend
docker compose -f deploy/docker-compose.split.yml logs -f --tail=200 postgres
docker compose -f deploy/docker-compose.split.yml logs -f --tail=100 deck
```

Each backend request logs: `http method=… path=… status=… dur_ms=…` (health-probe lines suppressed).  
Effective config is logged at boot: look for `config: …` lines — base URL, cookie_secure, SMTP mode, RAG target.

---

## Common alerts and responses

### `/api/health` → 503

The `db` field in the response body will have the Postgres error. Likely causes:

1. **Postgres container down**
   ```bash
   docker compose -f deploy/docker-compose.split.yml ps postgres
   docker compose -f deploy/docker-compose.split.yml logs --tail=50 postgres
   docker compose -f deploy/docker-compose.split.yml restart postgres
   ```
   Wait for the healthcheck to pass (`healthy` in `ps` output), then the backend reconnects automatically.

2. **Backend lost the DB connection transiently** — usually self-heals on the next request. If persistent, restart the backend:
   ```bash
   docker compose -f deploy/docker-compose.split.yml restart backend
   curl -s https://telawiki.com/api/health | jq
   ```

---

### High 5xx rate

Check backend logs for the failing endpoint and error:
```bash
docker compose -f deploy/docker-compose.split.yml logs -f --tail=500 backend | grep '"status":5'
```

Prometheus series: `tela_http_requests_total{status="5xx"}` (scrape `/metrics` with admin PAT).

Common causes:
- **Deck 5xx on deck-related endpoints** — `/api/pages/{id}/deck/*`: check `deck` container.
  ```bash
  docker compose -f deploy/docker-compose.split.yml logs --tail=100 deck
  docker compose -f deploy/docker-compose.split.yml restart deck
  ```
- **RAG/ask 503** — embedder or LLM unreachable; these 503s are expected when `TELA_RAG_EMBED_URL` / `TELA_LLM_URL` are unset. Only alert if they were previously working.
- **Atlas run errors** — see "Atlas run stuck" below.

---

### Atlas run stuck in `running`

On a clean restart, `ResumeDangling` automatically resumes runs left `running` — **restart the backend first**:
```bash
docker compose -f deploy/docker-compose.split.yml restart backend
```

If the run is genuinely stuck (bad source, LLM hung), force-fail it from inside the backend container:
```bash
docker compose -f deploy/docker-compose.split.yml exec backend \
  psql "$TELA_DATABASE_URL" -c \
  "UPDATE atlas_runs SET status='failed', err='manually cancelled', finished_at=tela_now() WHERE status='running';"
```
Then restart the backend to re-dispatch any queued `pending` runs.

To inspect stuck runs:
```bash
docker compose -f deploy/docker-compose.split.yml exec backend \
  psql "$TELA_DATABASE_URL" -c \
  "SELECT id, status, stage, err, started_at FROM atlas_runs WHERE status IN ('running','pending') ORDER BY id;"
```

---

### Disk full

**Check PG volume usage:**
```bash
docker system df -v | grep tela-pgdata
# Or from inside the container:
docker compose -f deploy/docker-compose.split.yml exec postgres \
  psql -U tela -d tela -c "SELECT pg_size_pretty(pg_database_size('tela'));"
```

**Biggest tables:**
```bash
docker compose -f deploy/docker-compose.split.yml exec postgres \
  psql -U tela -d tela -c \
  "SELECT relname, pg_size_pretty(pg_total_relation_size(oid)) FROM pg_class WHERE relkind='r' ORDER BY pg_total_relation_size(oid) DESC LIMIT 20;"
```

**Prune old atlas run data** (ingestion tables — `atlas_run_events`, `atlas_files`, `atlas_symbols`, `atlas_chunks` cascade-delete from `atlas_runs`):
```bash
docker compose -f deploy/docker-compose.split.yml exec postgres \
  psql -U tela -d tela -c \
  "DELETE FROM atlas_runs WHERE status IN ('succeeded','failed') AND finished_at < NOW() - INTERVAL '30 days';"
```

**Events table** — auto-GC'd every 6h (default 180-day retention). To lower retention, set in `deploy/.env` and restart the backend:
```
TELA_EVENTS_RETENTION_DAYS=90
```

**Registry volume** (images accumulate per-deploy tag):
```bash
docker compose -f deploy/docker-compose.registry.yml exec registry \
  registry garbage-collect /etc/docker/registry/config.yml
```

**PG vacuuming:**
```bash
docker compose -f deploy/docker-compose.split.yml exec postgres \
  psql -U tela -d tela -c "VACUUM ANALYZE;"
```

---

### Embedder / RAG returning 503

> **Note:** `rag` in `/api/health` reflects config (is `TELA_RAG_EMBED_URL` set?), not a live network ping.

1. **Check the embedder is reachable** from the host:
   ```bash
   curl -s "$TELA_RAG_EMBED_URL/api/ps"   # Ollama; should list loaded models
   ```
2. **Check the model is loaded:**
   ```bash
   curl -s "$TELA_RAG_EMBED_URL/api/tags" | jq '.models[].name'
   # Should include qwen3-embedding:0.6b (or your configured model)
   ```
3. **Private overlay network hostname issue** — the backend container may not resolve the embedder's hostname over a VPN/tailnet. Use the overlay **IP** in `TELA_RAG_EMBED_URL`, not the hostname. Edit `deploy/.env` and restart:
   ```bash
   docker compose -f deploy/docker-compose.split.yml restart backend
   ```
4. **If Ollama is down** — restart it on the embedder host. The backend auto-recovers; un-indexed pages are re-queued by the background index-health sweep.

---

## Service restart

```bash
cd ~/tela   # REMOTE_DIR on the deploy host

# Restart one service
docker compose -f deploy/docker-compose.split.yml restart backend
docker compose -f deploy/docker-compose.split.yml restart postgres
docker compose -f deploy/docker-compose.split.yml restart frontend
docker compose -f deploy/docker-compose.split.yml restart deck

# Verify health after restart
curl -s https://telawiki.com/api/health | jq
curl -s https://telawiki.com/api/version | jq
```

---

## Rollback

Images are tagged with commit SHAs in the on-box registry (`127.0.0.1:5000/tela-backend:<sha>`).  
Deploy a prior commit from your local machine (the one with `deploy/deploy.env` configured):

```bash
# Roll back backend only to a specific commit
make deploy-backend TELA_COMMIT=<prior-sha>

# Roll back all services
make deploy TELA_COMMIT=<prior-sha>
```

The health gate verifies `/api/version` reports the target commit after deploy.

**To list available tags in the registry** (on the deploy host):
```bash
curl -s http://127.0.0.1:5000/v2/tela-backend/tags/list | jq
```

---

## Escalation

```bash
robot ping --stream claude "incident: <brief description>"
```

---

## External Caddy edge

The shared Caddy lives outside the tela compose stack and owns TLS. To reload after a `sites.caddy` change:
```bash
docker exec <edge-container> caddy reload --config /etc/caddy/Caddyfile
```
(`make deploy-landing` does this automatically after syncing.)

Caddy logs for routing/TLS issues:
```bash
docker logs <edge-container> --tail=100 -f
```
