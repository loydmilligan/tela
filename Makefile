# Optional, untracked make-var overrides (deploy host/paths for the remote-deploy
# targets). Keeps personal values out of git — see the Remote deploy section.
-include deploy/deploy.env

COMPOSE := docker compose -f deploy/docker-compose.yml

# Auto-stamp git metadata into the backend image so GET /api/version reports
# real values instead of dev/unknown. `?=` defers to operator overrides (env
# or deploy/.env). `git describe --tags --always --dirty` falls back to a
# short SHA when no tags exist; the `|| echo` covers detached / non-git checkouts.
TELA_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
TELA_COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
EXPORT_BUILD := TELA_VERSION=$(TELA_VERSION) TELA_COMMIT=$(TELA_COMMIT)

# ── Local dev / test Postgres ───────────────────────────────────────────────
# The backend now requires Postgres (no more file SQLite). `dev-db` boots a
# single throwaway container shared by `be-dev` and `make test`; it persists
# between runs (just `docker start` if it already exists). `make test` provisions
# isolated throwaway databases on it (see internal/testdb), so it never collides
# with the `tela` dev database. Port 55433 avoids clashing with a host Postgres.
DEV_PG_CONTAINER := tela-dev-pg
DEV_DATABASE_URL := postgres://tela:tela@localhost:55433/tela?sslmode=disable
# Maintenance DSN the test harness connects to in order to CREATE/DROP per-test DBs.
TEST_DATABASE_URL := postgres://tela:tela@localhost:55433/postgres?sslmode=disable

# ── Remote deploy (no registry) ─────────────────────────────────────────────
# Two styles, both run FROM ANY machine; set the host(s) in your shell, on the
# command line (`make deploy-remote REMOTE=…`), or in untracked deploy/deploy.env:
#   build-on-remote → `make deploy` / `deploy-backend|frontend|landing`: SSH to
#                     DEPLOY_HOST, git pull + build there (needs a capable box).
#   build-local     → `make deploy-remote REMOTE=<host>`: build images HERE,
#                     `docker save | ssh <host> docker load` (no registry, no
#                     on-box build — good for small boxes / the split topology).
DEPLOY_HOST ?=
DEPLOY_DIR  ?= ~/proj/tela
PUBLIC_URL  ?= https://tela.cagdas.io

# build-local deploy (deploy-remote): the ssh host, the repo path on it, and the
# dir the external shared edge serves static from (landing + sites.caddy ride it).
# EDGE_CONTAINER, if set, is reloaded after the sync so the edge re-reads sites.caddy.
REMOTE         ?= $(DEPLOY_HOST)
REMOTE_DIR     ?= $(DEPLOY_DIR)
REMOTE_WEB     ?= /srv/web
EDGE_CONTAINER ?=

# RUN_REMOTE wraps a shell command so it runs on DEPLOY_HOST — but if we're
# ALREADY on it (running `make deploy` from the host's own shell), ssh-to-self is
# wasteful and needs key auth to localhost, so we run it locally instead.
# Usage in a recipe:  $(RUN_REMOTE) 'cd $(DEPLOY_DIR) && git pull --ff-only && …'
ifeq ($(shell hostname),$(DEPLOY_HOST))
  RUN_REMOTE = sh -c
else
  RUN_REMOTE = ssh $(DEPLOY_HOST)
endif

.PHONY: up down logs build clean dev be-dev fe-dev storybook help test-mcp-integration \
        test dev-db dev-db-clean setup backup restore \
        landing-dev landing-build landing-gate landing-clean \
        deploy deploy-backend deploy-frontend deploy-landing deploy-remote release-mcp reset-prod-db \
        health-gate

# setup: first-run convenience. Copies deploy/.env.example → deploy/.env and
# fills the three load-bearing secrets with `openssl rand -hex 32` so a
# self-hoster never ships blank/weak secrets. Refuses to clobber an existing
# .env. Edit the result (admin creds, public base URL, SMTP) before `make up`.
setup:
	@if [ -f deploy/.env ]; then \
	  echo "deploy/.env already exists — not overwriting. Delete it first to regenerate."; exit 1; fi
	@cp deploy/.env.example deploy/.env
	@for key in TELA_API_KEY_SECRET TELA_SHARE_SECRET TELA_PG_PASSWORD; do \
	  secret=$$(openssl rand -hex 32); \
	  sed -i "s|^$$key=.*|$$key=$$secret|" deploy/.env; \
	done
	@echo "Wrote deploy/.env with generated secrets."
	@echo "Next: edit deploy/.env (TELA_ADMIN_*, TELA_PUBLIC_BASE_URL, TELA_SMTP_*), then 'make up'."

help:
	@echo "tela — common targets"
	@echo "  make setup      # first run: write deploy/.env from the example with generated secrets"
	@echo "  make up         # build and start the stack (auto-stamps git version/commit) on http://localhost:8780"
	@echo "  make down       # stop the stack"
	@echo "  make logs       # tail logs from all services"
	@echo "  make backup     # dump Postgres to ./backups/tela-<timestamp>.sql"
	@echo "  make restore FILE=backups/...  # restore a dump"
	@echo "  make build      # rebuild images without starting (auto-stamps git version/commit)"
	@echo "  make clean      # stop and DELETE volumes (requires FORCE=1)"
	@echo "  make dev        # run backend + frontend in dev mode in parallel (no compose)"
	@echo "  make be-dev     # run backend in dev mode (go run; boots a local Postgres)"
	@echo "  make fe-dev     # run frontend in dev mode (vite)"
	@echo "  make test       # backend tests against a throwaway Postgres (boots dev-db)"
	@echo "  make dev-db     # boot the local dev/test Postgres container (:55433)"
	@echo "  make storybook  # run Storybook for the frontend"
	@echo "  make test-mcp-integration  # live MCP <-> backend E2E (boots stack, runs tests, tears down)"
	@echo "  make landing-dev    # run the marketing landing (Astro) dev server on :4321"
	@echo "  make landing-build  # build the static landing into landing/dist/"
	@echo "  make landing-gate   # run the landing production gates (a11y, tokens, motion, lighthouse)"
	@echo ""
	@echo "Remote deploy (set DEPLOY_HOST / REMOTE in deploy/deploy.env; run from any machine):"
	@echo "  make deploy           # pull + full 'make up' on DEPLOY_HOST, then verify /api/version commit"
	@echo "  make deploy-backend   # pull + rebuild ONLY the backend service, then verify /api/version"
	@echo "  make deploy-frontend  # pull + rebuild ONLY the frontend service"
	@echo "  make deploy-landing   # pull + landing-build + recreate proxy so it re-reads landing/dist"
	@echo "  make deploy-remote REMOTE=<host>  # build images locally + docker-load to a remote + up split stack (no registry)"
	@echo "  make reset-prod-db    # DESTROY + recreate the prod Postgres volume (requires FORCE=1)"
	@echo "  make release-mcp      # publish tela-mcp to npm (BUMP=patch|minor|major, default patch)"

# `up` builds the marketing landing first so the proxy's /srv/landing mount is
# populated (Caddy serves it at the apex). All images — including the proxy,
# which now bakes the landing build (deploy/proxy/Dockerfile) — build via
# compose, so `make up` needs only Docker, no host Node.
#
# The Caddyfile is still a single-file bind mount; compose won't recreate the
# proxy on a Caddyfile-only change, and the mount pins the OLD inode after a
# `git pull` rewrites the file. Force-recreate the proxy so it re-mounts the
# current Caddyfile.
up:
	$(EXPORT_BUILD) $(COMPOSE) up -d --build
	$(COMPOSE) up -d --force-recreate proxy

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f --tail=100

# backup: dump the Postgres DB to ./backups/tela-<timestamp>.sql via the running
# postgres container, using the container's own POSTGRES_USER/DB so no host
# credentials are needed. restore: load a dump back (drops + recreates objects;
# the dump is taken with --clean --if-exists). The sanctioned self-host data
# backup/restore path. Stop writers (or accept a slightly inconsistent snapshot)
# while dumping a live instance.
backup:
	@mkdir -p backups
	@out=backups/tela-$$(date +%Y%m%d-%H%M%S).sql; \
	  $(COMPOSE) exec -T postgres sh -c 'pg_dump -U "$$POSTGRES_USER" -d "$$POSTGRES_DB" --no-owner --clean --if-exists' > $$out && \
	  echo "wrote $$out"

restore:
	@test -n "$(FILE)" || { echo "usage: make restore FILE=backups/tela-YYYYMMDD-HHMMSS.sql"; exit 1; }
	@$(COMPOSE) exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"' < $(FILE) && \
	  echo "restored from $(FILE)"

build:
	$(EXPORT_BUILD) $(COMPOSE) build

clean:
ifeq ($(FORCE),1)
	$(COMPOSE) down -v
else
	@echo "make clean removes named volumes (tela-pgdata — the POSTGRES DATABASE — caddy-data, caddy-config) and destroys all data."
	@echo "Re-run with FORCE=1 to confirm:   make clean FORCE=1"
	@exit 1
endif

# dev-db: idempotently ensure the local dev/test Postgres is running. Starts the
# existing container if present, else creates it. Waits for pg_isready so the
# first `go run` / `go test` doesn't race the server's startup.
dev-db:
	@docker start $(DEV_PG_CONTAINER) >/dev/null 2>&1 || \
	  docker run -d --name $(DEV_PG_CONTAINER) \
	    -e POSTGRES_USER=tela -e POSTGRES_PASSWORD=tela -e POSTGRES_DB=tela \
	    -p 55433:5432 pgvector/pgvector:pg17 >/dev/null
	@echo "waiting for postgres…"; \
	for i in $$(seq 1 30); do \
	  docker exec $(DEV_PG_CONTAINER) pg_isready -U tela -d tela >/dev/null 2>&1 && break; \
	  sleep 0.5; \
	done

be-dev: dev-db
	cd backend && TELA_DATABASE_URL="$(DEV_DATABASE_URL)" go run ./cmd/tela

# Backend unit/integration tests against a real Postgres (the testdb harness
# clones a fresh throwaway database per test). Boots dev-db first. Runs the
# blocks-manifest gate first so a stale agent guide / uncovered block fails CI.
test: blocks-gate dev-db
	cd backend && TELA_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./...

# ── Block authoring manifest (editor slash menu + agent guide source) ───────
# Source of truth: frontend/src/components/app/blocks-manifest.json. blocks-gen
# regenerates the go:embed copy the backend renders into the MCP authoring
# guide; blocks-gate verifies it's in sync AND that every renderer plugin is
# covered (a new block can't ship invisible to agents).
blocks-gen:
	node scripts/blocks-manifest.mjs --write

blocks-gate:
	node scripts/blocks-manifest.mjs --check

# Stop + remove the local dev/test Postgres (and its data).
dev-db-clean:
	-docker rm -f $(DEV_PG_CONTAINER)

fe-dev:
	cd frontend && npm run dev

dev:
	@echo "Starting backend (be-dev) and frontend (fe-dev) in parallel — Ctrl-C stops both."
	@$(MAKE) -j2 be-dev fe-dev

storybook:
	cd frontend && npm run storybook

# ── Marketing landing (standalone Astro in landing/) ────────────────────────
# Separate static build from the app (backend/ + frontend/). Deployed as static
# files; the apex / serves this, the app keeps /login, /spaces, /share, etc.
landing-dev:
	cd landing && npm run dev

landing-build:
	cd landing && PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 npm ci && npm run build

landing-gate:
	cd landing && npm run gate

landing-clean:
	rm -rf landing/dist landing/node_modules landing/.astro

# M16.C.2 — live integration test for the MCP server against a real backend.
# Boots the stack with -p tela-test (isolated volumes), seeds deterministic
# admin credentials, builds the MCP, runs vitest.integration.config, then
# tears everything down. The `trap` ensures we always clean up even when the
# test step exits non-zero.
TEST_COMPOSE := docker compose -p tela-test -f deploy/docker-compose.yml -f deploy/docker-compose.test.yml
test-mcp-integration:
	@set -eu; \
	TELA_PUBLIC_BASE_URL="http://localhost:18780"; \
	TELA_SHARE_SECRET="$$(openssl rand -hex 32)"; \
	TELA_API_KEY_SECRET="$$(openssl rand -hex 32)"; \
	TELA_ADMIN_USERNAME="testadmin"; \
	TELA_ADMIN_PASSWORD="testpassword123"; \
	export TELA_PUBLIC_BASE_URL TELA_SHARE_SECRET TELA_API_KEY_SECRET TELA_ADMIN_USERNAME TELA_ADMIN_PASSWORD $(EXPORT_BUILD); \
	trap '$(TEST_COMPOSE) down -v --remove-orphans >/dev/null 2>&1 || true' EXIT INT TERM; \
	echo "[test-mcp-integration] booting tela-test stack…"; \
	$(TEST_COMPOSE) up -d --build --wait; \
	echo "[test-mcp-integration] building MCP server (npm ci → prepare → tsc)…"; \
	npm --prefix mcp ci --silent; \
	echo "[test-mcp-integration] running live vitest suite…"; \
	TELA_BASE_URL="$$TELA_PUBLIC_BASE_URL" \
	TELA_ADMIN_USERNAME="$$TELA_ADMIN_USERNAME" \
	TELA_ADMIN_PASSWORD="$$TELA_ADMIN_PASSWORD" \
	  npm --prefix mcp run test:integration

# ── Remote deploy targets ───────────────────────────────────────────────────
# The build-on-remote targets run from ANY machine: RUN_REMOTE ssh's into
# DEPLOY_HOST (or runs locally if we're already on it). They `git pull --ff-only`
# so deploy never silently diverges from a force-push — a non-fast-forward fails
# loudly instead.
#
# PRECONDITION: push your commit before deploying. The health gate compares the
# locally-captured HEAD ($(TELA_COMMIT), evaluated on THIS machine before ssh)
# against what /api/version reports after the host pulls + rebuilds. If you
# forgot to push, the host pulls nothing, the running commit won't match, and the
# gate fails — exactly the "commit ≠ deploy" trap we want to make impossible.

# health-gate: poll PUBLIC_URL/api/version until the reported `commit` equals
# EXPECT_COMMIT (default: this checkout's HEAD), or fail. This is the permanent
# fix for "pushed but prod still runs the old binary". jq is used when present
# for robust JSON parsing; otherwise we fall back to grep/sed so the gate never
# hard-depends on jq. 12 tries × 5s ≈ 1 min — covers a backend rebuild+restart.
EXPECT_COMMIT ?= $(TELA_COMMIT)
health-gate:
	@set -eu; \
	expect="$(EXPECT_COMMIT)"; \
	echo "[health-gate] expecting commit '$$expect' at $(PUBLIC_URL)/api/version"; \
	for i in $$(seq 1 12); do \
	  body="$$(curl -fsS --max-time 10 "$(PUBLIC_URL)/api/version" 2>/dev/null || true)"; \
	  if [ -n "$$body" ]; then \
	    if command -v jq >/dev/null 2>&1; then \
	      got="$$(printf '%s' "$$body" | jq -r '.commit // empty')"; \
	    else \
	      got="$$(printf '%s' "$$body" | grep -o '"commit"[[:space:]]*:[[:space:]]*"[^"]*"' | sed -E 's/.*:[[:space:]]*"([^"]*)"/\1/')"; \
	    fi; \
	    if [ "$$got" = "$$expect" ]; then \
	      echo "✓ deploy verified — /api/version reports commit $$got"; exit 0; \
	    fi; \
	    echo "  …reported '$$got' (want '$$expect'), retry $$i/12"; \
	  else \
	    echo "  …unreachable, retry $$i/12"; \
	  fi; \
	  sleep 5; \
	done; \
	echo "✗ deploy NOT verified: $(PUBLIC_URL)/api/version never reported commit '$$expect'." >&2; \
	echo "  (Did you push? Or did 'make up' on $(DEPLOY_HOST) fail to rebuild the backend?)" >&2; \
	exit 1

# deploy: full stack. Pull on the deploy host, run the normal `make up`
# (landing-build + build/recreate every service + force-recreate proxy), verify.
deploy:
	$(RUN_REMOTE) 'cd $(DEPLOY_DIR) && git pull --ff-only && make up'
	@$(MAKE) health-gate EXPECT_COMMIT=$(TELA_COMMIT)

# deploy-backend: rebuild + recreate ONLY the backend container (faster than a
# full `up` when only Go changed). Must pass EXPORT_BUILD so the rebuilt image
# carries the right version/commit ldflags — without it /api/version would
# report dev/unknown and the health gate would (correctly) fail. The git
# metadata is computed ON THE REMOTE (it's the deploying host's HEAD after pull),
# so EXPORT_BUILD is expanded inside the remote command, not on the laptop.
deploy-backend:
	$(RUN_REMOTE) 'cd $(DEPLOY_DIR) && git pull --ff-only && \
	  TELA_VERSION="$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
	  TELA_COMMIT="$$(git rev-parse --short HEAD 2>/dev/null || echo unknown)" \
	  docker compose -f deploy/docker-compose.yml up -d --build backend'
	@$(MAKE) health-gate EXPECT_COMMIT=$(TELA_COMMIT)

# deploy-frontend: rebuild + recreate ONLY the frontend (static nginx) container.
# No version stamp (the frontend isn't ldflag-stamped) and no health gate
# (/api/version reflects the backend, not the FE bundle).
deploy-frontend:
	$(RUN_REMOTE) 'cd $(DEPLOY_DIR) && git pull --ff-only && \
	  docker compose -f deploy/docker-compose.yml up -d --build frontend'

# deploy-landing: the landing is baked into the proxy image
# (deploy/proxy/Dockerfile), so "deploying" it = pull + rebuild the proxy image
# on the deploy host. No host Node build step. --force-recreate is REQUIRED: the Caddyfile
# is bind-mounted (not baked), and a Caddyfile-only change cache-hits the image —
# without --force-recreate the proxy keeps running the old in-memory config and
# the edit silently never applies. This is also the target for Caddyfile changes.
deploy-landing:
	$(RUN_REMOTE) 'cd $(DEPLOY_DIR) && git pull --ff-only && \
	  docker compose -f deploy/docker-compose.yml up -d --build --force-recreate proxy'

# deploy-remote: build-local deploy of the SPLIT stack to a remote host (for the
# split topology behind an external shared edge — see docs/self-hosting.md). The
# remote does NOT build: this builds the two custom images locally (native), then
# `docker save | ssh REMOTE docker load` (no registry — good for small boxes),
# syncs the landing + sites.caddy to the dir the edge serves static from
# (REMOTE_WEB), pulls the repo + `up`s the split compose, reloads the external
# edge if EDGE_CONTAINER is set, then verifies /api/version. Postgres pulls a
# public image; gotenberg is configured (in deploy/.env) as a remote renderer.
#   make deploy-remote REMOTE=<ssh-host> [REMOTE_WEB=/srv/web] [EDGE_CONTAINER=edge-caddy]
deploy-remote:
	@test -n "$(REMOTE)" || { echo "set REMOTE=<ssh-host> (or in deploy/deploy.env)"; exit 1; }
	@echo "[deploy-remote] building backend+frontend ($(TELA_COMMIT)) locally…"
	docker build --build-arg VERSION=$(TELA_VERSION) --build-arg COMMIT=$(TELA_COMMIT) -t tela-backend:latest backend
	docker build -t tela-frontend:latest frontend
	$(MAKE) landing-build
	@echo "[deploy-remote] streaming images → $(REMOTE)…"
	docker save tela-backend:latest tela-frontend:latest | ssh $(REMOTE) docker load
	@echo "[deploy-remote] syncing landing + sites.caddy → $(REMOTE):$(REMOTE_WEB)…"
	ssh $(REMOTE) 'mkdir -p $(REMOTE_WEB)/tela-landing $(REMOTE_WEB)/tela'
	rsync -a --delete landing/dist/ $(REMOTE):$(REMOTE_WEB)/tela-landing/
	rsync -a deploy/proxy/sites.caddy $(REMOTE):$(REMOTE_WEB)/tela/sites.caddy
	@echo "[deploy-remote] pull + up split stack…"
	ssh $(REMOTE) 'cd $(REMOTE_DIR) && git pull --ff-only && \
	  docker compose -f deploy/docker-compose.split.yml up -d'
	@if [ -n "$(EDGE_CONTAINER)" ]; then \
	  ssh $(REMOTE) 'docker exec $(EDGE_CONTAINER) caddy reload --config /etc/caddy/Caddyfile' || true; fi
	@$(MAKE) health-gate EXPECT_COMMIT=$(TELA_COMMIT)

# reset-prod-db: GUARDED, DESTRUCTIVE. Drops the prod Postgres volume and brings
# it back empty so the backend re-runs migrations from scratch. Prod data is
# disposable by design (pre-1.0); this is the sanctioned reset. Requires FORCE=1,
# same as `clean`. Runs on the deploy host: stop the backend (releases
# connections) + postgres, delete the named volume, then `up` so postgres
# re-initializes and the backend re-migrates against a fresh DB.
reset-prod-db:
ifeq ($(FORCE),1)
	$(RUN_REMOTE) 'cd $(DEPLOY_DIR) && \
	  docker compose -f deploy/docker-compose.yml stop backend postgres && \
	  docker compose -f deploy/docker-compose.yml rm -f postgres && \
	  docker volume rm tela_tela-pgdata && \
	  make up'
	@$(MAKE) health-gate EXPECT_COMMIT=$(TELA_COMMIT)
else
	@echo "reset-prod-db DESTROYS the production Postgres volume (tela_tela-pgdata) — ALL wiki data is lost."
	@echo "Re-run with FORCE=1 to confirm:   make reset-prod-db FORCE=1"
	@exit 1
endif

# ── MCP npm release (ships to npm, NOT to the deploy host) ──────────────────
# Publishes the tela-mcp package. BUMP selects the semver bump (default patch):
#   make release-mcp BUMP=minor
# Flow (from docs/architecture.md + mcp/README): npm version with the
# tela-mcp-v* tag prefix (so MCP tags don't collide with app tags), publish
# public, then push the commit+tag from repo root.
# HAZARDS (see architecture.md):
#  (1) `npm version` can silently skip — we verify the tag was created and abort
#      if not, rather than publishing an untagged build.
#  (2) npm strips a `bin` value starting with `./` — package.json must use
#      "tela-mcp": "dist/server.js" (no leading ./). We dry-run publish first.
#  (3) registry @latest has CDN lag — verify with `npm view … versions` after.
#  (4) ESM entrypoint guard must realpathSync both sides or npx-via-symlink
#      exits 0 silently — that's a code concern, covered by mcp's own smoke test.
BUMP ?= patch
release-mcp:
	@set -eu; \
	echo "[release-mcp] bumping tela-mcp ($(BUMP)) and publishing to npm…"; \
	cd mcp && \
	new="$$(npm version $(BUMP) --tag-version-prefix=tela-mcp-v)"; \
	echo "[release-mcp] npm version reported: $$new"; \
	if ! git tag --list 'tela-mcp-v*' | grep -qx "tela-mcp-v$${new#tela-mcp-v}"; then \
	  echo "✗ npm version did not create tag $$new — aborting before publish." >&2; exit 1; \
	fi; \
	echo "[release-mcp] dry-run publish (catches bad bin paths / files globs)…"; \
	npm publish --dry-run --access public; \
	npm publish --access public; \
	cd .. && git push --follow-tags; \
	echo "✓ published $$new — verify CDN with: npm view tela-mcp versions --json"
