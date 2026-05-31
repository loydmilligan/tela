COMPOSE := docker compose -f deploy/docker-compose.yml

# Auto-stamp git metadata into the backend image so GET /api/version reports
# real values instead of dev/unknown. `?=` defers to operator overrides (env
# or deploy/.env). `git describe --tags --always --dirty` falls back to a
# short SHA when no tags exist; the `|| echo` covers detached / non-git checkouts.
TELA_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
TELA_COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
EXPORT_BUILD := TELA_VERSION=$(TELA_VERSION) TELA_COMMIT=$(TELA_COMMIT)

.PHONY: up down logs build clean dev be-dev fe-dev storybook help test-mcp-integration \
        landing-dev landing-build landing-gate landing-clean

help:
	@echo "tela — common targets"
	@echo "  make up         # build and start the stack (auto-stamps git version/commit) on http://localhost:8780"
	@echo "  make down       # stop the stack"
	@echo "  make logs       # tail logs from all services"
	@echo "  make build      # rebuild images without starting (auto-stamps git version/commit)"
	@echo "  make clean      # stop and DELETE volumes (requires FORCE=1)"
	@echo "  make dev        # run backend + frontend in dev mode in parallel (no compose)"
	@echo "  make be-dev     # run backend in dev mode (go run)"
	@echo "  make fe-dev     # run frontend in dev mode (vite)"
	@echo "  make storybook  # run Storybook for the frontend"
	@echo "  make test-mcp-integration  # live MCP <-> backend E2E (boots stack, runs tests, tears down)"
	@echo "  make landing-dev    # run the marketing landing (Astro) dev server on :4321"
	@echo "  make landing-build  # build the static landing into landing/dist/"
	@echo "  make landing-gate   # run the landing production gates (a11y, tokens, motion, lighthouse)"

# `up` builds the marketing landing first so the proxy's /srv/landing mount is
# populated (Caddy serves it at the apex). The app images build via compose.
#
# The proxy runs an unbuilt caddy image with a single-file Caddyfile bind mount;
# compose won't recreate it on a Caddyfile-only change, and a single-file mount
# pins the OLD inode after a `git pull` rewrites the file — so Caddy keeps the
# stale config. Force-recreate the proxy so it re-mounts the current Caddyfile
# (and the freshly built landing/dist).
up: landing-build
	$(EXPORT_BUILD) $(COMPOSE) up -d --build
	$(COMPOSE) up -d --force-recreate proxy

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f --tail=100

build:
	$(EXPORT_BUILD) $(COMPOSE) build

clean:
ifeq ($(FORCE),1)
	$(COMPOSE) down -v
else
	@echo "make clean removes named volumes (tela-data, caddy-data, caddy-config) and destroys data."
	@echo "Re-run with FORCE=1 to confirm:   make clean FORCE=1"
	@exit 1
endif

be-dev:
	cd backend && go run ./cmd/tela

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
