# deck — Slidev render sidecar (render-only)

A tela **deck** is a **page** whose body **is Slidev markdown** (whole page —
a page is *either* a deck or a doc, set by `deck: true`; slides are never inline
in regular doc content). This service renders that markdown to **static premium
output** — per-slide PNGs + a PDF/PPTX. There is **no interactive SPA** and
nothing Slidev runs in the viewer's browser: tela presents the rendered images
in its own simple full-screen viewer. That keeps this service tiny.

> Quality note: the themes in `themes/` are **placeholders**. The premium,
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

## Parser vs render (two tiers)

The heavy path (PNG/PDF/PPTX) shells out to the Slidev **CLI + headless
Chromium**. But **structure** — slide count, titles, layouts, speaker notes,
detected features (KaTeX/Monaco/Mermaid/Tweet) — comes from `@slidev/parser`
**in-process, no Chromium, in milliseconds**. So:

- Theme injection sets `theme` on the parsed first-slide YAML doc and
  re-serializes (`parse` → `prettifySlide` → `stringify`) — no frontmatter
  regex surgery.
- `/parse` returns deck structure with no render at all (powers an editor
  outline / slide navigator).
- `/render` and `/export/*` **preflight** with the parser first, so a malformed
  deck fails fast as `400` instead of dying deep in a multi-minute Chromium run.

## Contract

```
GET  /themes                  -> [{ name, label, description }]   (theme selector)
POST /parse                   body: slidev markdown -> { count, slides:[{no,title,layout,note}], features, errors }
POST /render?theme=<name>     body: slidev markdown -> { id, count, theme, slides:[url] }
POST /export/<pdf|pptx>?theme body: slidev markdown -> the file bytes
GET  /d/<id>/<file>           -> a rendered slide PNG / the PDF / the PPTX
GET  /health                  -> ok
```

`/render` returns one PNG per slide. Render/export are cached by content hash
(keyed on `RENDER_VERSION` + theme + markdown), so re-rendering an unchanged
deck is instant; `/parse` is cheap enough to skip caching. `DECK_CACHE`
(default `./cache`, `/data` in the image) holds the rendered output.

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
