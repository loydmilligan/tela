# Builds the static marketing landing (Astro) and bakes it into the Caddy image
# so `make up` needs only Docker — no host Node toolchain. Previously the landing
# was built on the host (`npm ci && npm run build`) and bind-mounted into Caddy,
# which made a Docker-only self-hoster's first `make up` fail.
#
# Build context is the repo root (see docker-compose.yml: context: ..), so the
# COPY paths are repo-relative. The Caddyfile itself stays bind-mounted by
# compose, so config tweaks don't need an image rebuild — but they DO need a
# container recreate to load (Caddy holds the config in memory and we run no
# file-watcher): recreate via the standalone stack (`make up` / `docker compose
# up -d proxy`). Note `make deploy-landing` is the SPLIT-shape target (rsync +
# external-edge reload) — it never touches this container.

# ── Stage 1: build the landing ──────────────────────────────────────────────
FROM node:22-alpine AS landing
WORKDIR /landing
COPY landing/package.json landing/package-lock.json ./
RUN PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 npm ci
COPY landing/ ./
RUN npm run build

# ── Stage 2: Caddy serving the built landing ────────────────────────────────
FROM caddy:2-alpine
COPY --from=landing /landing/dist /srv/landing
# Bake the config as a DEFAULT so the published image is self-contained for
# image-only deploys (one-click app stores: Umbrel/CasaOS/Render/DO/etc.) that
# can't bind-mount the repo. The standalone compose bind-mounts ./proxy/Caddyfile
# + ./proxy/sites.caddy read-only over these, so `make up` is unchanged — the
# bind-mount wins; this only takes effect when nothing is mounted.
COPY deploy/proxy/Caddyfile /etc/caddy/Caddyfile
COPY deploy/proxy/sites.caddy /etc/caddy/sites.caddy
