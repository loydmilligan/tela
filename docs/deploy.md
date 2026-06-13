# Deploying tela

The production deploy (tela.cagdas.io) is the **split topology** behind a shared
external edge. This doc is the one place for "how do I build & deploy."

## TL;DR

```bash
make deploy            # build → push changed layers → recreate → verify. THE command.
```

That's it. Partial/faster variants and the fallback:

```bash
make deploy-backend    # only the Go backend changed (build + push + recreate + health-gate)
make deploy-frontend   # only the SPA changed
make deploy-landing    # only the marketing landing / sites.caddy changed (static, no image)
make deploy-offline    # no-registry fallback (full-image docker save | ssh load)
```

`make up` is unrelated — that's the **standalone** all-in-one stack for local /
self-hosting (see [self-hosting.md](self-hosting.md)). It is not how the split
prod box is deployed.

**Push your commit first.** `make deploy` tags the image with your local `HEAD`
commit and the health gate fails if `/api/version` doesn't report it — so a
forgotten `git push` (or a build that didn't land) is caught, not silently shipped.

> [!WARNING]
> **Images build from the WORKING TREE, not from `HEAD`.** `docker build … backend`
> / `frontend` and `landing-build` use the current directory as the build context,
> so **uncommitted changes ship**. The commit tag is cosmetic (only the backend's
> `/api/version` is gated; the frontend/landing have no such check), so a `-dirty`
> build can silently carry someone else's in-progress edits. When two people work
> in one tree, deploy only the surface you own: `make deploy-backend` rebuilds the
> Go image from `backend/` alone (a clean context if only the frontend has WIP),
> and vice-versa for `deploy-frontend`. Commit or stash unrelated WIP before a full
> `make deploy`.

## Why a registry (and not `docker save`)

`docker save | ssh docker load` streams the **whole** image every time — no
dedup — so a one-file change still pushes ~150 MB over the uplink. A registry
**push negotiates**: it HEADs each layer and skips the ones the box already has,
so only the changed layer (a few MB) crosses the wire. Same local build, far
less transfer. That's the entire reason `make deploy` is registry-based.

The build still happens on the deploying machine — the box **never builds**, it
only pulls. (That property is why the split box can be small.)

## How `make deploy` works

```
build (local)  →  push (only changed layers)  →  box pulls + recreates  →  health-gate
```

1. **`registry-up`** (a prerequisite): `git pull --ff-only` on the box so its
   compose + `sites.caddy` source is current, then `up -d` the on-box registry
   ([`docker-compose.registry.yml`](../deploy/docker-compose.registry.yml)) —
   idempotent, a no-op once it's running.
2. **Build** `tela-backend` and `tela-frontend` locally, each tagged
   `:<commit>` (immutable, for rollback) and `:latest`.
3. **Push** over a transient SSH tunnel (`make` opens a control-master socket,
   pushes, closes it on exit — even on failure). The laptop's `:5000` reaches
   the box's loopback registry; the box later pulls from its own `127.0.0.1:5000`.
4. **Sync** the landing build + `sites.caddy` to `REMOTE_WEB` (the external edge
   serves/imports them).
5. **Recreate** the split stack: `TELA_BACKEND_IMAGE=…:<commit>
   TELA_FRONTEND_IMAGE=…:<commit> docker compose -f docker-compose.split.yml up -d`.
   `pull_policy: always` pulls the just-pushed tag; a changed digest recreates
   the container.
6. **Health-gate** polls `PUBLIC_URL/api/version` until `commit` == local HEAD.

## Registry: the on-box `registry:2`

- A standalone compose project (`name: tela-registry`) so it's independent infra
  — bring it up once, it survives app deploys.
- **Bound to `127.0.0.1` only.** Never on the box's public interface; the push
  rides an authenticated SSH tunnel, the box pulls over loopback — so there's no
  registry auth/TLS to manage.
- Layers live on the `tela-registry-data` volume. Every `:<commit>` tag
  accumulates, so the volume grows slowly over many deploys. Prune occasionally
  if it matters (the registry supports GC; or just recreate the volume — images
  re-push on the next deploy).

## Moving the registry elsewhere

The flow is parametrized so the registry isn't pinned to the box:

| Var | Default | Meaning |
| --- | --- | --- |
| `TELA_REGISTRY` | `127.0.0.1:5000` | host:port prefix images are tagged/pushed under |
| `TELA_REGISTRY_PORT` | `5000` | port the on-box registry publishes (loopback) |
| `REG_TUNNEL` | `1` | open an SSH tunnel for the push (on-box loopback registry) |

To use e.g. GHCR instead of the on-box registry, in `deploy/deploy.env`:

```make
TELA_REGISTRY := ghcr.io/zcag
REG_TUNNEL    := 0
```

…and make sure both the deploying machine (`docker login ghcr.io`) and the box
can reach it. Nothing else in the flow changes. The split compose's
`TELA_BACKEND_IMAGE` / `TELA_FRONTEND_IMAGE` (see
[`docker-compose.split.yml`](../deploy/docker-compose.split.yml)) can also be
pointed anywhere directly if you want full control of the refs.

## Fallback: `make deploy-offline`

No registry involved. Builds locally, `docker save | ssh docker load`s the full
images, then recreates the split stack with the loaded bare-name images
(`TELA_PULL_POLICY=never`). Use it for **first boot** (before the registry
exists) or if the registry is ever unhealthy. Slow on a thin uplink — that's the
problem `make deploy` solves.

## Build speed

Builds use **BuildKit** (the Makefile exports `DOCKER_BUILDKIT=1`; the
Dockerfiles declare `# syntax=docker/dockerfile:1`). Two things keep them fast:

- **Layer ordering** — deps are copied + installed before source, so a code-only
  change reuses the `go mod download` / `npm ci` layers.
- **Cache mounts** — `RUN --mount=type=cache` persists the Go module + **Go build**
  cache and the npm cache *across* builds, independent of the image layer cache.
  So a `go.sum`/`package-lock` bump re-fetches only new deps, and an incremental
  backend build recompiles only changed packages (~4s vs ~30s cold).

**Prerequisite: Docker with buildx.** Cache mounts need BuildKit, which modern
Docker drives through the buildx plugin. If `docker buildx version` fails, install
it — `pacman -S docker-buildx` (Arch), your distro's `docker-buildx-plugin`, or
drop the release binary into `~/.docker/cli-plugins/docker-buildx` (no sudo).
Without it the build errors instead of silently falling back to the slow legacy
builder.

## Config

`REMOTE`, `REMOTE_DIR`, `REMOTE_WEB`, `EDGE_CONTAINER`, and any registry
overrides live in untracked `deploy/deploy.env` (make-var overrides). App
secrets + host config live in `deploy/.env` (see
[`deploy/.env.example`](../deploy/.env.example)). Both are gitignored.
