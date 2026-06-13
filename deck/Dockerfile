# tela deck render sidecar — Slidev + headless Chromium for export.
FROM node:20-slim

ENV PORT=3344 \
    DECK_CACHE=/data \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm install --omit=dev

# Chromium (+ system deps) for `slidev export`.
RUN npx playwright install --with-deps chromium

COPY . .

VOLUME /data
EXPOSE 3344
CMD ["node", "server.mjs"]
