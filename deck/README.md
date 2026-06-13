# deck — Slidev render sidecar (render-only)

A tela **deck** is a **page** whose body **is Slidev markdown** (whole page —
a page is *either* a deck or a doc, set by `deck: true`; slides are never inline
in regular doc content). This service renders that markdown to **static premium
output** — per-slide PNGs + a PDF/PPTX. There is **no interactive SPA** and
nothing Slidev runs in the viewer's browser: tela presents the rendered images
in its own simple full-screen viewer. That keeps this service tiny.

> Quality note: the theme in `theme/` is a **placeholder**. The premium,
> pp-grade theme is a separate, deliberate design pass (later, via the
> frontend-design skill). This directory is the *plumbing*, not the look.

## Why a service (and why render-only)

Slidev is a Vue/Vite tool, not an embeddable library — it has to *render*.
Rendering lives in its own Node process; tela stores the markdown, this service
renders it, tela serves + presents the output. The stored markdown never
references the theme path — the service injects tela's theme (overriding any
`theme:` in the deck headmatter), so the look is controlled centrally and decks
stay portable. **Render-only** (no per-deck SPA build, no SPA serving) is the
deliberate simplification: present is just images.

## Contract

```
POST /render            body: slidev markdown -> { id, count, slides:[url], pdf }
POST /export/pdf        body: slidev markdown -> application/pdf
POST /export/pptx       body: slidev markdown -> .pptx
GET  /d/<id>/<n>.png    -> a rendered slide image (Present + thumbnails)
GET  /d/<id>/deck.pdf   -> the exported PDF
GET  /health           -> ok
```

`/render` returns one PNG per slide. Everything is cached by content hash, so
re-rendering an unchanged deck is instant. `DECK_CACHE` (default `./cache`,
`/data` in the image) holds the rendered output.

## Run

```bash
npm install
npm start                     # :3344
# or the container:
docker build -t tela-deck . && docker run -p 3344:3344 tela-deck
```

## tela integration (next layer, not built yet)

- **Page type:** a page is a deck when `deck: true` (frontmatter / props —
  `pagemd`/PageProperties already support it). Its body is Slidev markdown.
- **Edit:** a deck page edits as **plain markdown** (not the rich Milkdown
  editor — deck and doc are separate paths; no shared slide code).
- **Present:** tela renders the deck (POST `/render`), then shows the per-slide
  PNGs in a simple **full-screen image viewer** (arrow-key nav). PNGs double as
  thumbnails. Render-on-save keeps Present instant.
- **Export:** PDF / PPTX proxy `/export/*`.
- **Backend:** a deck route proxies this sidecar (mirrors `pdf_export.go`, the
  gotenberg proxy). **Compose:** add a `deck` service alongside gotenberg; the
  backend reaches it at `http://deck:3344`.
