# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder
WORKDIR /src

# Build metadata injected via -ldflags into internal/api package vars. See
# backend/internal/api/version.go for the receiving side. Defaults preserve
# the same dev/unknown signal as `go run ./cmd/tela`.
ARG VERSION=dev
ARG COMMIT=unknown

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# Cache mounts persist the module + Go build caches across builds (independent of
# the image layer cache), so a go.sum change re-downloads only new modules and an
# incremental build recompiles only changed packages — not the whole graph.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags="-s -w \
        -X github.com/zcag/tela/backend/internal/api.Version=${VERSION} \
        -X github.com/zcag/tela/backend/internal/api.Commit=${COMMIT} \
        -X github.com/zcag/tela/backend/internal/api.BuildTime=${BUILD_TIME}" \
      -o /out/tela ./cmd/tela \
 && mkdir -p /out/data \
 && chown -R 65532:65532 /out/data

# Runtime: Alpine rather than distroless/static because the atlas git source
# connector shells out to the `git` CLI (clone / ls-remote / diff / ls-files) —
# distroless has no git. ca-certificates covers HTTPS clones + the remote
# LLM/embed calls. (A future pure-Go git, e.g. go-git, could restore a
# distroless runtime.) uid 65532 keeps the nonroot user id stable across the
# image change so /data ownership is unchanged.
FROM alpine:3.20
RUN apk add --no-cache git ca-certificates \
 && adduser -D -H -u 65532 nonroot
COPY --from=builder /out/tela /tela
COPY --from=builder --chown=65532:65532 /out/data /data
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/tela"]
