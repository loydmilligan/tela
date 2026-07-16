# syntax=docker/dockerfile:1

FROM node:20-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json ./
# Cache mount persists npm's download cache across builds, so a package-lock
# change re-fetches only the changed tarballs instead of the whole tree.
RUN --mount=type=cache,target=/root/.npm npm ci
COPY . .
RUN npm run build

FROM nginx:alpine AS runtime
# Provenance labels. The frontend is a static bundle with no /api/version to ask,
# so before these an image was completely opaque — identifying one meant
# content-hashing its files against the tree. Now:
#   docker inspect <img> --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'
ARG VERSION=dev
ARG COMMIT=unknown
LABEL org.opencontainers.image.title="tela-frontend" \
      org.opencontainers.image.source="https://github.com/loydmilligan/tela" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.version="${VERSION}"
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 80
