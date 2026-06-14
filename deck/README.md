# deck — Slidev render sidecar

A tela **deck** is a **page** whose body **is Slidev markdown** (whole page —
a page is *either* a deck or a doc, set by `deck: true`). Two output paths off the
same parse + theme-injection core:

- **Present = the live interactive SPA** (`slidev build`, pure Vite, no Chromium):
  the real Slidev app — presenter mode, overview, drawing, live clicks. tela serves
  it page-scoped + membership-gated and opens it in a new tab.
- **Export / agent-preview / thumbnails = static frames** (`slidev export`,
  headless Chromium): PNG / PDF / PPTX.

> The look lives entirely in **[`slidev-theme-tahta`](https://github.com/zcag/tahta)**
> (an npm dependency) — a token-driven design system with style **variants**.
> This service owns **no** layouts/styles; it just injects the theme + a per-deck
> visual config. This directory is the *plumbing*, not the look.

## Theme injection (shared by both paths)

The stored markdown never references the theme — the service injects
`theme: slidev-theme-tahta` + a per-deck `themeConfig` (`variant`/`accent`/`lang`)
+ `mdc: true` (tahta is MDC-authored), via the parser (`parse` → set on the
first-slide YAML doc → `prettifySlide` → `stringify`), overriding any user values
for those keys. The variant catalog comes from the theme's own `variants.json`;
tela hardcodes nothing visual. The built SPA uses history routing; `/spa` serves
`index.html` as a fallback for client routes (slide N, presenter) so deep links
and refreshes resolve.

`@slidev/parser` also powers `/parse` (structure, no render — editor outline) and
the parser-based preflight on `/render` + `/export`.

## Contract

```
GET  /themes                      -> [{ name, label, scheme, description }]  (tahta variants)
GET  /authoring                   -> { guide, variants, themeVersion }  (tahta's AGENTS.md verbatim — MCP deck guide)
POST /parse                       body: markdown -> { count, slides:[{no,title,layout,note}], features, errors }
POST /lint                        body: markdown -> { ok, errors, warnings, issues[] }  (tahta validator)
POST /spa?base&file&variant…      body: markdown -> one file of the built interactive SPA (build-if-needed, cached, in-flight-locked)
POST /render?variant&accent&lang  body: markdown -> { id, count, variant, slides:[url] }
POST /export/<pdf|pptx>?variant…  body: markdown -> the file bytes
GET  /d/<id>/<file>               -> a rendered slide PNG / PDF / PPTX
GET  /health                      -> ok
```

`/spa` builds the deck into a static SPA under `base` (the path tela serves it at,
so asset URLs resolve), cached by content hash, and returns the requested file.
tela's backend proxies each browser asset GET → `/spa` (gated by page membership).

`/render` returns one PNG per click-step (`--with-clicks`). Render/export are
cached by content hash (keyed on `RENDER_VERSION` + the visual config +
markdown), so re-rendering an unchanged deck is instant; `/parse` is cheap enough
to skip caching. `DECK_CACHE` (default `./cache`, `/data` in the image) holds the
rendered output. The variant catalog is read from `slidev-theme-tahta/variants.json`.

## Run

```bash
npm install
npm start                     # :3344
# or the container:
docker build -t tela-deck . && docker run -p 3344:3344 tela-deck
```

## tela integration (built)

- **Page type:** a page is a deck when `props.deck === true`. Its body is
  Slidev markdown; deck and doc are separate paths (no shared slide code).
- **Edit:** a deck page edits as **plain markdown** (raw textarea, not the rich
  Milkdown editor) — see `PageEditor`'s `isDeck` branch.
- **Present:** `DeckPresenter` calls `GET /api/pages/{id}/deck` → this sidecar's
  `/render` → shows the per-slide PNGs in a full-screen image viewer (arrow-key
  nav). PNGs double as thumbnails.
- **Export:** `GET /api/pages/{id}/deck.pdf` proxies `/export/pdf`.
- **Backend:** `backend/internal/api/deck_render.go` proxies this sidecar
  (mirrors `pdf_export.go`, the gotenberg proxy). Asset reads go through the
  public `GET /api/deck/d/{renderId}/{file}` (content-addressed, immutable).
- **Compose:** a `deck` service alongside gotenberg; the backend reaches it at
  `http://deck:3344` (`TELA_DECK_URL`).

## Roadmap (parser, not yet wired into the UI)

- **Slide outline in the editor** — a backend `…/deck/outline` proxy over
  `/parse` feeds a live slide navigator while editing (no render).
- **Presenter notes / instant structure** — `DeckPresenter` shows count + titles
  from `/parse` immediately, and exposes per-slide speaker notes.
- **Incremental render** — hash each parsed slide; re-render only changed slides
  (`slidev export --range`) instead of the whole deck on save.
