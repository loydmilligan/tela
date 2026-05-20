COMPOSE := docker compose -f deploy/docker-compose.yml

# Auto-stamp git metadata into the backend image so GET /api/version reports
# real values instead of dev/unknown. `?=` defers to operator overrides (env
# or deploy/.env). `git describe --tags --always --dirty` falls back to a
# short SHA when no tags exist; the `|| echo` covers detached / non-git checkouts.
TELA_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
TELA_COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
EXPORT_BUILD := TELA_VERSION=$(TELA_VERSION) TELA_COMMIT=$(TELA_COMMIT)

.PHONY: up down logs build clean dev be-dev fe-dev storybook help

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

up:
	$(EXPORT_BUILD) $(COMPOSE) up -d --build

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
