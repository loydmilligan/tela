# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tela ./cmd/tela

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/tela /tela
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/tela"]
