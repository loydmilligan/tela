# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder
WORKDIR /src

# Build metadata injected via -ldflags into internal/api package vars. See
# backend/internal/api/version.go for the receiving side. Defaults preserve
# the same dev/unknown signal as `go run ./cmd/tela`.
ARG VERSION=dev
ARG COMMIT=unknown

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags="-s -w \
        -X github.com/zcag/tela/backend/internal/api.Version=${VERSION} \
        -X github.com/zcag/tela/backend/internal/api.Commit=${COMMIT} \
        -X github.com/zcag/tela/backend/internal/api.BuildTime=${BUILD_TIME}" \
      -o /out/tela ./cmd/tela \
 && mkdir -p /out/data \
 && chown -R 65532:65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/tela /tela
COPY --from=builder --chown=nonroot:nonroot /out/data /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/tela"]
