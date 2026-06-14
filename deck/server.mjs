// tela deck render sidecar — Slidev as a RENDER-ONLY service.
//
// A tela deck is a page whose body IS Slidev markdown. This service renders that
// markdown to static premium output (per-slide PNGs + PDF/PPTX). The entire look
// lives in the `slidev-theme-tahta` theme package — tela owns no layouts/styles;
// it only declares a per-deck visual config (variant/accent/lang), which this
// service injects into the deck headmatter. There is no interactive SPA and
// nothing Slidev runs in the viewer's browser — tela presents the rendered
// images in its own simple full-screen viewer. That keeps this service tiny.
//
//   GET  /themes                      -> [{ name, label, scheme, description }]  (tahta variants)
//   GET  /authoring                   -> { rules, themeConfig, layouts, components, variants }  (theme contract, for the MCP deck guide)
//   POST /lint                        body: markdown -> { ok, errors, warnings, issues:[{slide,level,field?,message}] }  (tahta validator)
//   POST /parse                       body: markdown -> { count, slides:[{no,title,layout,note}], features, errors }
//   POST /render?variant&accent&lang  body: markdown -> { id, count, slides:[url], variant }
//   POST /export/<pdf|pptx>?variant…  body: markdown -> the file bytes
//   GET  /d/<id>/<file>               -> a rendered slide PNG / the PDF / the PPTX
//   GET  /health                      -> ok
//
// Two tiers: structure (count/titles/layouts/notes/features) comes from
// @slidev/parser in-process — no Chromium — and powers /parse, theme injection,
// and a fast preflight on /render + /export. Only pixels go through the CLI.
//
// Hardening: cache key folds in RENDER_VERSION + visual config (so a variant/
// accent/lang change busts stale renders); renders are bounded by a small concurrency gate + a
// per-render timeout (Chromium is heavy). Cached by content hash. Runs as a
// compose sidecar (the gotenberg pattern); tela proxies it. The container is
// treated as untrusted-code execution (Slidev compiles deck markdown) — isolate
// it at the network/Compose level.

import http from 'node:http'
import { createHash } from 'node:crypto'
import { writeFile, mkdir, readdir } from 'node:fs/promises'
import { existsSync, createReadStream, statSync } from 'node:fs'
import { join, extname, dirname, normalize } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createRequire } from 'node:module'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { parse, stringify, prettifySlide, detectFeatures } from '@slidev/parser/core'
import { lint as tahtaLint } from 'slidev-theme-tahta/lint.mjs'

const exec = promisify(execFile)
const require = createRequire(import.meta.url)
const ROOT = dirname(fileURLToPath(import.meta.url))
const CACHE = process.env.DECK_CACHE || join(ROOT, 'cache')
const WORK = join(CACHE, 'work')
const SLIDEV = join(ROOT, 'node_modules', '.bin', 'slidev')
const PORT = Number(process.env.PORT || 3344)
const MAX_CONCURRENCY = Number(process.env.DECK_CONCURRENCY || 2)

// Bump when the theme or render pipeline changes so cached decks rerender.
// r4: inject mdc:true (headmatter/cover slide rendered blank without it).
const RENDER_VERSION = 'r4'

// The look lives entirely in the theme package — tela owns no layouts/styles.
// Variant catalog + the themeConfig keys come from the theme's own manifests.
const THEME_PKG = 'slidev-theme-tahta'
const DEFAULT_VARIANT = 'editorial'
const VARIANT_CATALOG = require(`${THEME_PKG}/variants.json`).variants
const VARIANTS = new Set(VARIANT_CATALOG.map((v) => v.id))

// The full authoring contract straight from the theme package's manifests
// (layouts + fields + examples + components + rules + variants) — served at
// /authoring so tela's backend can render the agent deck-authoring guide. tela
// defines none of this; the theme owns it.
const AUTHORING = (() => {
  const m = require(`${THEME_PKG}/layouts.json`)
  return {
    rules: m.rules || [],
    themeConfig: m.themeConfig || {},
    layouts: m.layouts || [],
    components: m.components || [],
    variants: VARIANT_CATALOG,
  }
})()

await mkdir(WORK, { recursive: true })

const MIME = { '.png': 'image/png', '.pdf': 'application/pdf', '.pptx': 'application/vnd.openxmlformats-officedocument.presentationml.presentation' }
const EXPORT_MIME = { pdf: MIME['.pdf'], pptx: MIME['.pptx'] }

const hash = (s) => createHash('sha256').update(s).digest('hex').slice(0, 16)
// Cache key folds in the full visual config so a variant/accent/lang change rerenders.
const cfgKey = (c) => `${pickVariant(c.variant)}|${c.accent || ''}|${c.lang || ''}`
const deckId = (md, cfg) => hash(`${RENDER_VERSION}|${cfgKey(cfg)}|${md}`)
const deckDir = (id) => join(CACHE, 'd', id)
const pickVariant = (v) => (v && VARIANTS.has(v) ? v : DEFAULT_VARIANT)

// The deck render config tela controls per-deck — the only inputs to the theme.
const reqConfig = (params) => ({
  variant: params.get('variant') || '',
  accent: params.get('accent') || '',
  lang: params.get('lang') || '',
})

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

// Point the deck at the tahta theme and apply tela's per-deck visual config
// (variant/accent/lang) — the whole look lives in the theme package; tela just
// declares which variant. Done through the parser: set `theme` + `themeConfig`
// on the first slide's YAML doc and re-serialize, overriding any user values for
// the keys tela manages while preserving the rest. The stored deck markdown is
// untouched — this only shapes the ephemeral render input.
function tahtaThemeConfig(cfg, existing) {
  const tc = { ...(existing || {}), variant: pickVariant(cfg.variant) }
  if (cfg.accent) tc.accent = cfg.accent
  if (cfg.lang) tc.lang = cfg.lang
  return tc
}

function withTheme(data, cfg) {
  const head = data.slides[0]
  if (head && head.frontmatterDoc && head.frontmatterDoc.contents) {
    const cur = head.frontmatterDoc.get('themeConfig')
    head.frontmatterDoc.set('theme', THEME_PKG)
    head.frontmatterDoc.set('themeConfig', tahtaThemeConfig(cfg, cur && cur.toJSON ? cur.toJSON() : cur))
    // tahta is MDC-authored (its layouts read frontmatter via MDC; the headmatter
    // slide renders blank without it). Default it on unless the deck set it.
    if (head.frontmatterDoc.get('mdc') === undefined) head.frontmatterDoc.set('mdc', true)
    prettifySlide(head) // rebuild head.raw from the mutated YAML doc (stringify reads raw)
    return stringify(data)
  }
  // No headmatter block to edit — prepend one.
  const tc = tahtaThemeConfig(cfg)
  const lines = [`theme: ${THEME_PKG}`, 'mdc: true', 'themeConfig:', `  variant: ${tc.variant}`]
  if (tc.accent) lines.push(`  accent: ${JSON.stringify(tc.accent)}`)
  if (tc.lang) lines.push(`  lang: ${tc.lang}`)
  return `---\n${lines.join('\n')}\n---\n\n${data.raw}`
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

async function slidevExport(md, cfg, format, outPath) {
  const entry = join(WORK, `${hash(cfgKey(cfg) + format + md)}.md`)
  await writeFile(entry, withTheme(await parseDeck(md), cfg))
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

// Slidev `export --format png --with-clicks` names frames `<slide>-<click>.png`,
// zero-padded (e.g. 001-01.png, 001-02.png, 002-01.png); without clicks it's
// still per-slide padded. Match both and order by (slide, click).
const isFrame = (f) => /^\d+(-\d+)?\.png$/.test(f)
const frameOrder = (f) => f.replace(/\.png$/, '').split('-').map(Number)
const frameCmp = (a, b) => {
  const [as, ac = 0] = frameOrder(a)
  const [bs, bc = 0] = frameOrder(b)
  return as - bs || ac - bc
}
const listFrames = async (dir) => (existsSync(dir) ? (await readdir(dir)).filter(isFrame) : [])

async function renderImages(md, cfg) {
  const id = deckId(md, cfg)
  const dir = deckDir(id)
  if (!(await listFrames(dir)).length) {
    await mkdir(dir, { recursive: true })
    await slidevExport(md, cfg, 'png', dir)
  }
  const pngs = (await listFrames(dir)).sort(frameCmp)
  // Frames may exceed logical slides (--with-clicks). Ship the logical outline
  // (titles + speaker notes) and a frame→slide map so the presenter view can
  // show the right note per frame.
  const data = await parseDeck(md)
  return {
    id,
    variant: pickVariant(cfg.variant),
    count: pngs.length,
    slides: pngs.map((f) => `/d/${id}/${f}`),
    outline: outlineSlides(data),
    slideForFrame: frameSlideMap(data, pngs.length),
  }
}

async function renderFile(md, cfg, format) {
  const file = join(deckDir(deckId(md, cfg)), `deck.${format}`)
  if (!existsSync(file)) {
    await mkdir(dirname(file), { recursive: true })
    await slidevExport(md, cfg, format, file)
  }
  return file
}

// The style catalog comes straight from the theme package's manifest — tela
// defines no themes of its own.
function listVariants() {
  return VARIANT_CATALOG.map((v) => ({ name: v.id, label: v.label, scheme: v.scheme, description: v.description }))
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
    const cfg = reqConfig(url.searchParams)

    if (req.method === 'GET' && path === '/themes') {
      res.writeHead(200, { 'content-type': 'application/json' }).end(JSON.stringify(listVariants()))
    } else if (req.method === 'GET' && path === '/authoring') {
      // The theme's full layout/component/variant contract, for tela's MCP guide.
      res.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'public, max-age=300' }).end(JSON.stringify(AUTHORING))
    } else if (req.method === 'POST' && path === '/lint') {
      // tahta's own structural validator (it owns the layout/field semantics).
      // Parse with the real parser, then lint per-slide frontmatter. Slide numbers
      // surfaced 1-based to match /parse + the editor outline.
      const md = await readBody(req)
      const data = await parseDeck(md)
      const r = await tahtaLint(data.slides.map((s) => s.frontmatter || {}))
      const issues = (r.issues || []).map((it) => ({ ...it, slide: (it.slide ?? 0) + 1 }))
      res.writeHead(200, { 'content-type': 'application/json' }).end(JSON.stringify({ ...r, issues }))
    } else if (req.method === 'POST' && path === '/parse') {
      // Cheap structure + features, no render. Powers the editor outline + preflight.
      const md = await readBody(req)
      res.writeHead(200, { 'content-type': 'application/json' }).end(JSON.stringify(deckMeta(await parseDeck(md), md)))
    } else if (req.method === 'POST' && path === '/render') {
      const md = await readBody(req)
      await preflight(md) // parse first — a bad deck fails fast as 400, not a slow 502
      const manifest = await renderImages(md, cfg) // resolve BEFORE writing headers
      res.writeHead(200, { 'content-type': 'application/json' }).end(JSON.stringify(manifest))
    } else if (req.method === 'POST' && path.startsWith('/export/')) {
      const format = path.slice('/export/'.length)
      if (!EXPORT_MIME[format]) return void res.writeHead(400).end('format must be pdf or pptx')
      const md = await readBody(req)
      await preflight(md)
      const file = await renderFile(md, cfg, format)
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

server.listen(PORT, () => console.log(`tela deck sidecar on :${PORT} — ${THEME_PKG} variants: [${[...VARIANTS]}], concurrency ${MAX_CONCURRENCY}`))
