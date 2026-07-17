# tela deck render sidecar — Slidev + headless Chromium for export.
FROM node:20-slim

ENV PORT=3344 \
    DECK_CACHE=/data \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1

WORKDIR /app

# Chromium (+ system deps) in its OWN early layer, keyed only to the pinned
# Playwright version — so changing app deps (slidev/tahta/echarts/…) no longer
# invalidates and re-pushes this ~340MB layer. Keep this version in lockstep with
# the `playwright-chromium` pin in package.json so the browser revision matches.
RUN npx --yes playwright@1.60.0 install --with-deps chromium

# App deps + code: the layers that actually change on a normal deploy. The
# browser download is skipped (env above) — playwright-chromium reuses the
# Chromium installed above at PLAYWRIGHT_BROWSERS_PATH.
COPY package.json package-lock.json* ./
RUN npm install --omit=dev

COPY . .

# Provenance. Declared HERE, below every expensive layer, on purpose: COMMIT
# changes on every build, and an ARG invalidates the cache from the point it is
# declared onward — hoisting these to the top would bust the ~340MB Chromium
# layer above on every deploy and re-push it, defeating the layer split this
# file is built around. LABEL is metadata only, so it costs nothing here.
#
# Without these the Makefile's --build-arg VERSION/COMMIT was silently dropped
# (Docker discards build-args a Dockerfile never declares), so the deck image
# carried NO provenance at all — `docker inspect` returned empty, which reads as
# "no answer" but gets acted on as "nothing to see". An honest `unknown` beats a
# label that is structurally incapable of being right.
ARG VERSION=dev
ARG COMMIT=unknown
LABEL org.opencontainers.image.title="tela-deck" \
      org.opencontainers.image.source="https://github.com/loydmilligan/tela" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.version="${VERSION}"

VOLUME /data
EXPOSE 3344
CMD ["node", "server.mjs"]
