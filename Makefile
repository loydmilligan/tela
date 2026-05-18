COMPOSE := docker compose -f deploy/docker-compose.yml

.PHONY: up down logs build clean dev be-dev fe-dev storybook help

help:
	@echo "tela — common targets"
	@echo "  make up         # build and start the stack (backend + frontend + proxy) on http://localhost:8780"
	@echo "  make down       # stop the stack"
	@echo "  make logs       # tail logs from all services"
	@echo "  make build      # rebuild images without starting"
	@echo "  make clean      # stop and DELETE volumes (requires FORCE=1)"
	@echo "  make dev        # run backend + frontend in dev mode in parallel (no compose)"
	@echo "  make be-dev     # run backend in dev mode (go run)"
	@echo "  make fe-dev     # run frontend in dev mode (vite)"
	@echo "  make storybook  # run Storybook for the frontend"

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f --tail=100

build:
	$(COMPOSE) build

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
