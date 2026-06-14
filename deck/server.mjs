// tela deck render sidecar — Slidev as a RENDER-ONLY service.
//
// A tela deck is a page whose body IS Slidev markdown. This service renders that
// markdown to static premium output (per-slide PNGs + PDF/PPTX), applying ONE OF
// tela's themes (chosen per deck via ?theme=). There is no interactive SPA and
// nothing Slidev runs in the viewer's browser — tela presents the rendered
// images in its own simple full-screen viewer. That keeps this service tiny.
//
//   GET  /themes                      -> [{ name, label, description }]   (for the editor's selector)
//   POST /parse                       body: markdown -> { count, slides:[{no,title,layout,note}], features, errors }
//   POST /render?theme=<name>         body: markdown -> { id, count, slides:[url], theme }
//   POST /export/<pdf|pptx>?theme=<name>  body: markdown -> the file bytes
//   GET  /d/<id>/<file>               -> a rendered slide PNG / the PDF / the PPTX
//   GET  /health                      -> ok
//
// Two tiers: structure (count/titles/layouts/notes/features) comes from
// @slidev/parser in-process — no Chromium — and powers /parse, theme injection,
// and a fast preflight on /render + /export. Only pixels go through the CLI.
//
// Hardening: cache key folds in RENDER_VERSION + theme (so a theme/engine change
// busts stale renders); renders are bounded by a small concurrency gate + a
// per-render timeout (Chromium is heavy). Cached by content hash. Runs as a
// compose sidecar (the gotenberg pattern); tela proxies it. The container is
// treated as untrusted-code execution (Slidev compiles deck markdown) — isolate
// it at the network/Compose level.

import http from 'node:http'
import { createHash } from 'node:crypto'
import { writeFile, mkdir, readdir, readFile } from 'node:fs/promises'
import { existsSync, createReadStream, statSync, readdirSync } from 'node:fs'
import { join, extname, dirname, normalize } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { parse, stringify, prettifySlide, detectFeatures } from '@slidev/parser/core'

const exec = promisify(execFile)
const ROOT = dirname(fileURLToPath(import.meta.url))
const THEMES_DIR = join(ROOT, 'themes')
const CACHE = process.env.DECK_CACHE || join(ROOT, 'cache')
const WORK = join(CACHE, 'work')
const SLIDEV = join(ROOT, 'node_modules', '.bin', 'slidev')
const PORT = Number(process.env.PORT || 3344)
const MAX_CONCURRENCY = Number(process.env.DECK_CONCURRENCY || 2)

// Bump when the theme set or the render pipeline changes so cached decks rerender.
// r2: render with --with-clicks (one frame per click-step) so build animations survive.
const RENDER_VERSION = 'r2'
const DEFAULT_THEME = 'default'
const THEMES = new Set(
  readdirSync(THEMES_DIR, { withFileTypes: true }).filter((d) => d.isDirectory()).map((d) => d.name),
)

await mkdir(WORK, { recursive: true })

const MIME = { '.png': 'image/png', '.pdf': 'application/pdf', '.pptx': 'application/vnd.openxmlformats-officedocument.presentationml.presentation' }
const EXPORT_MIME = { pdf: MIME['.pdf'], pptx: MIME['.pptx'] }

const hash = (s) => createHash('sha256').update(s).digest('hex').slice(0, 16)
const deckId = (md, theme) => hash(`${RENDER_VERSION}|${theme}|${md}`)
const deckDir = (id) => join(CACHE, 'd', id)
const pickTheme = (t) => (t && THEMES.has(t) ? t : DEFAULT_THEME)

// Parse deck markdown once via @slidev/parser (in-process — no Chromium, so
// milliseconds). The result drives theme injection, preflight validation, and
// the /parse metadata endpoint. `errors` carries any frontmatter/YAML problems.
const parseDeck = (md) => parse(md, 'deck.md')

// One outline entry per LOGICAL slide (title, layout, speaker note) — pure
// parse, no pixels. Shared by /parse and the render manifest.
function outlineSlides(data) {
  return data.slides.map((s, i) => ({
    no: i + 1,
    title: s.title || '',
    layout: (s.frontmatter && s.frontmatter.layout) || (i === 0 ? 'cover' : 'default'),
    note: s.note || '',
  }))
}

// Structure for the /parse endpoint and editor outline: the slide list plus
// detected feature flags (KaTeX/Monaco/Mermaid/Tweet) and any parse errors.
function deckMeta(data, md) {
  return {
    count: data.slides.length,
    slides: outlineSlides(data),
    features: detectFeatures(md),
    errors: data.errors || [],
  }
}

// Best-effort count of extra click-steps a slide adds (v-click/v-clicks/v-after,
// or an explicit `clicks:` frontmatter). Exact for the common no-click case;
// may drift for runtime-computed clicks (components, v-clicks `every`). Used only
// to map rendered frames back to logical slides for presenter notes.
function slideClicks(s) {
  if (s.frontmatter && Number.isInteger(s.frontmatter.clicks)) return s.frontmatter.clicks
  const m = (s.content || '').match(/\bv-clicks?\b|\bv-after\b|<v-clicks?\b/g)
  return m ? m.length : 0
}

// Map each rendered frame (--with-clicks emits one PNG per click-step) back to
// its logical slide index, so the presenter can show the right speaker note.
// Identity when there are no clicks (frames === slides); a monotonic clamp if
// the heuristic disagrees with the real frame count.
function frameSlideMap(data, frameCount) {
  const map = []
  data.slides.forEach((s, i) => {
    for (let k = 0; k <= slideClicks(s); k++) map.push(i)
  })
  if (map.length === frameCount) return map
  return Array.from({ length: frameCount }, (_, i) => Math.min(i, data.slides.length - 1))
}

// Parse-validate before a render: surfaces a malformed deck (e.g. broken YAML
// headmatter) as a fast 400 with the parser's message, instead of letting it
// fail deep inside a multi-minute Chromium export and bubble up as an opaque 502.
async function preflight(md) {
  let data
  try {
    data = await parseDeck(md)
  } catch (e) {
    // The parser is lenient, but a hard YAML/syntax failure throws — make it a 400.
    throw Object.assign(new Error(String(e?.message || e)), { status: 400 })
  }
  if (data.errors && data.errors.length) {
    const msg = data.errors.map((e) => (e.row != null ? `line ${e.row}: ${e.message}` : e.message)).join('; ')
    throw Object.assign(new Error(msg), { status: 400 })
  }
  return data
}

// Inject the chosen theme into the deck headmatter (overriding any user `theme:`)
// so the stored markdown stays portable and the look is controlled centrally.
// Done through the parser — set `theme` on the first slide's YAML document and
// re-serialize — instead of fragile frontmatter regex surgery.
function withTheme(data, theme) {
  const path = join(THEMES_DIR, theme)
  const head = data.slides[0]
  if (head && head.frontmatterDoc && head.frontmatterDoc.contents) {
    head.frontmatterDoc.set('theme', path)
    prettifySlide(head) // rebuild head.raw from the mutated YAML doc (stringify reads raw)
    return stringify(data)
  }
  // No headmatter block to edit — prepend one.
  return `---\ntheme: ${path}\n---\n\n${data.raw}`
}

// Bounded concurrency — Chromium renders are heavy; an unbounded burst OOMs.
let active = 0
const waiters = []
function acquire() {
  return new Promise((resolve) => {
    const grab = () => {
      if (active < MAX_CONCURRENCY) {
        active++
        resolve(() => {
          active--
          waiters.shift()?.()
        })
      } else {
        waiters.push(grab)
      }
    }
    grab()
  })
}

async function slidevExport(md, theme, format, outPath) {
  const entry = join(WORK, `${hash(theme + format + md)}.md`)
  await writeFile(entry, withTheme(await parseDeck(md), theme))
  const release = await acquire()
  try {
    // --with-clicks: one frame per click-step, so v-click/v-clicks/v-motion/
    // Magic Move build-ups survive instead of collapsing to the final state.
    await exec(SLIDEV, ['export', entry, '--format', format, '--output', outPath, '--with-clicks', '--timeout', '60000'], {
      cwd: ROOT, timeout: 240_000, maxBuffer: 1 << 24,
    })
  } finally {
    release()
  }
}

async function renderImages(md, theme) {
  const id = deckId(md, theme)
  const dir = deckDir(id)
  if (!existsSync(join(dir, '1.png'))) {
    await mkdir(dir, { recursive: true })
    await slidevExport(md, theme, 'png', dir) // -> 1.png, 2.png, ...
  }
  const pngs = (await readdir(dir)).filter((f) => /^\d+\.png$/.test(f)).sort((a, b) => parseInt(a) - parseInt(b))
  // Frames may exceed logical slides (--with-clicks). Ship the logical outline
  // (titles + speaker notes) and a frame→slide map so the presenter view can
  // show the right note per frame.
  const data = await parseDeck(md)
  return {
    id,
    theme,
    count: pngs.length,
    slides: pngs.map((f) => `/d/${id}/${f}`),
    outline: outlineSlides(data),
    slideForFrame: frameSlideMap(data, pngs.length),
  }
}

async function renderFile(md, theme, format) {
  const file = join(deckDir(deckId(md, theme)), `deck.${format}`)
  if (!existsSync(file)) {
    await mkdir(dirname(file), { recursive: true })
    await slidevExport(md, theme, format, file)
  }
  return file
}

async function listThemes() {
  const out = []
  for (const name of THEMES) {
    let meta = { name, label: name, description: '' }
    try {
      meta = { ...meta, ...JSON.parse(await readFile(join(THEMES_DIR, name, 'theme.json'), 'utf8')) }
    } catch { /* no metadata */ }
    out.push({ name: meta.name, label: meta.label, description: meta.description })
  }
  return out
}

function serveStatic(res, id, name) {
  const dir = deckDir(id)
  const file = normalize(join(dir, name))
  if (!file.startsWith(dir) || !existsSync(file) || statSync(file).isDirectory()) return void res.writeHead(404).end()
  res.writeHead(200, { 'content-type': MIME[extname(file)] || 'application/octet-stream', 'cache-control': 'public, max-age=31536000, immutable' })
  createReadStream(file).pipe(res)
}

async function readBody(req) {
  const chunks = []
  for await (const c of req) chunks.push(c)
  return Buffer.concat(chunks).toString('utf8')
}

const server = http.createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://x')
    const path = url.pathname
    const theme = pickTheme(url.searchParams.get('theme'))

    if (req.method === 'GET' && path === '/themes') {
      res.writeHead(200, { 'content-type': 'application/json' }).end(JSON.stringify(await listThemes()))
    } else if (req.method === 'POST' && path === '/parse') {
      // Cheap structure + features, no render. Powers the editor outline + preflight.
      const md = await readBody(req)
      res.writeHead(200, { 'content-type': 'application/json' }).end(JSON.stringify(deckMeta(await parseDeck(md), md)))
    } else if (req.method === 'POST' && path === '/render') {
      const md = await readBody(req)
      await preflight(md) // parse first — a bad deck fails fast as 400, not a slow 502
      const manifest = await renderImages(md, theme) // resolve BEFORE writing headers
      res.writeHead(200, { 'content-type': 'application/json' }).end(JSON.stringify(manifest))
    } else if (req.method === 'POST' && path.startsWith('/export/')) {
      const format = path.slice('/export/'.length)
      if (!EXPORT_MIME[format]) return void res.writeHead(400).end('format must be pdf or pptx')
      const md = await readBody(req)
      await preflight(md)
      const file = await renderFile(md, theme, format)
      res.writeHead(200, { 'content-type': EXPORT_MIME[format] })
      createReadStream(file).pipe(res)
    } else if (req.method === 'GET' && path.startsWith('/d/')) {
      const [, , id, name] = path.split('/')
      serveStatic(res, id, name || '')
    } else if (path === '/health') {
      res.writeHead(200).end('ok')
    } else {
      res.writeHead(404).end()
    }
  } catch (e) {
    // A render that already started streaming can't have headers rewritten —
    // log and drop the connection instead of crashing the process.
    if (res.headersSent) {
      console.error('deck error after headers sent:', e?.stack || e)
      return void res.destroy()
    }
    if ((e?.status || 500) === 400) {
      res.writeHead(400, { 'content-type': 'application/json' }).end(JSON.stringify({ error: 'parse', message: String(e.message || e) }))
    } else {
      res.writeHead(500, { 'content-type': 'text/plain' }).end(String(e?.stack || e))
    }
  }
})

server.listen(PORT, () => console.log(`tela deck sidecar on :${PORT} — themes: [${[...THEMES]}], concurrency ${MAX_CONCURRENCY}`))
