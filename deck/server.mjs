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
//   GET  /authoring                   -> { guide, variants, themeVersion }  (tahta's AGENTS.md verbatim, for the MCP deck guide)
//   POST /lint                        body: markdown -> { ok, errors, warnings, issues:[{slide,level,field?,message}] }  (tahta validator)
//   POST /spa?base&file&variant…      body: markdown -> one file of the built interactive SPA (build-if-needed, cached)
//   POST /parse                       body: markdown -> { count, slides:[{no,title,layout,note}], features, errors }
//   POST /render?variant&accent&lang  body: markdown -> { id, count, slides:[url], variant }
//   POST /cover?variant&accent&lang   body: markdown -> { url, count }  (first slide only — cover/OG)
//   POST /export/<pdf|pptx>?variant…  body: markdown -> the file bytes
//   GET  /d/<id>/<file>               -> a rendered slide PNG / the PDF / the PPTX
//   GET  /health                      -> ok
//
// Two tiers: structure (count/titles/layouts/notes/features) comes from
// @slidev/parser in-process — no Chromium — and powers /parse, theme injection,
// and a fast preflight on /render + /export. Only pixels go through the CLI.
//
// Hardening: cache key folds in RENDER_VERSION + theme version + visual config +
// the markdown (so a pipeline/theme/variant/source change busts stale renders —
// see CACHE_EPOCH); renders are bounded by a small concurrency gate + a
// per-render timeout (Chromium is heavy). Cached by content hash. Runs as a
// compose sidecar (the gotenberg pattern); tela proxies it. The container is
// treated as untrusted-code execution (Slidev compiles deck markdown) — isolate
// it at the network/Compose level.

import http from 'node:http'
import { createHash } from 'node:crypto'
import { writeFile, mkdir, readdir, rm } from 'node:fs/promises'
import { existsSync, createReadStream, statSync, readFileSync } from 'node:fs'
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

// Bump when the theme or render pipeline changes so cached decks re-render/re-build.
// r6: history-mode SPA + index.html fallback (hash mode broke routes under the base).
// r7: inject a router guard that undoes Slidev's base double-prepend on programmatic
//     nav (the "404 on slide 2" bug under a sub-path --base). See SPA_NAV_FIX below.
const RENDER_VERSION = 'r7'

// The look lives entirely in the theme package — tela owns no layouts/styles.
// Variant catalog + the themeConfig keys come from the theme's own manifests.
const THEME_PKG = 'slidev-theme-tahta'
const DEFAULT_VARIANT = 'editorial'
const THEME_DIR = dirname(require.resolve(`${THEME_PKG}/package.json`))
const THEME_VERSION = require(`${THEME_PKG}/package.json`).version
const VARIANT_CATALOG = require(`${THEME_PKG}/variants.json`).variants
const VARIANTS = new Set(VARIANT_CATALOG.map((v) => v.id))

// The agent authoring contract is OWNED BY THE THEME: tahta ships AGENTS.md
// (auto-generated from its own layouts.json/variants.json) — the full, current
// reference (rules, variants, universal fields, every layout + component with
// fields/props/examples). We serve it VERBATIM so the guide can't drift as tahta
// grows: a new layout/component/field appears the moment tahta is bumped, with
// zero tela changes. We pass through only `guide` (that markdown) + `variants`
// (structured — tela validates the `variant` prop + drives the picker against it).
// tahta also ships OPTIONAL capability modules (modules/modules.json + the .md
// fragments) — prompt addenda a consumer appends to the core guide ONLY when that
// capability is in play (a brand to honor / an image tool available), so the
// always-served core stays lean. We pass them through verbatim (id/when/adds +
// the fragment text); the backend decides when to surface each. Theme-owned, so
// the set grows with tahta and can't drift.
const AUTHORING = (() => {
  let guide = ''
  try {
    guide = readFileSync(join(THEME_DIR, 'AGENTS.md'), 'utf8')
  } catch {
    guide = '' // older theme without AGENTS.md → backend uses its fallback
  }
  let modules = []
  try {
    const manifest = require(`${THEME_PKG}/modules/modules.json`)
    modules = (manifest.modules || []).map((m) => ({
      id: m.id,
      when: m.when,
      adds: m.adds,
      text: readFileSync(join(THEME_DIR, m.file), 'utf8'),
    }))
  } catch {
    modules = [] // older theme without capability modules → backend omits them
  }
  return { guide, variants: VARIANT_CATALOG, modules, themeVersion: THEME_VERSION }
})()

const SPA = join(CACHE, 'spa') // built interactive SPAs, one dir per buildId
await mkdir(WORK, { recursive: true })

// Slidev auto-loads `<root>/setup/main.{ts,js,mts,mjs}` and runs its default export
// with the app context ({ app, router }). The build root is WORK (the dir holding the
// entry markdown), so one shared setup file applies to every deck build.
//
// SPA_NAV_FIX: we serve each deck's SPA under a sub-path (`--base /api/pages/<id>/
// deck/spa/`). Slidev's getSlidePath() prepends import.meta.env.BASE_URL to the path
// it pushes, but vue-router's history ALSO prepends base on push — so programmatic
// navigation (next/prev/goto/presenter) doubles the base and lands on NotFound (the
// "404 on slide 2" bug). <RouterLink> is unaffected (it pushes base-relative paths).
// This guard strips the duplicated base prefix so the router matches the real route.
// No-ops at root base (BASE_URL === '/'), e.g. Chromium export builds.
await mkdir(join(WORK, 'setup'), { recursive: true })
await writeFile(join(WORK, 'setup', 'main.mjs'), `// Injected by the tela deck sidecar — not part of any deck. See SPA_NAV_FIX in server.mjs.
export default function ({ router }) {
  const base = import.meta.env.BASE_URL
  if (!base || base === '/') return
  router.beforeEach((to) => {
    if (to.path.startsWith(base) && to.path.length > base.length) {
      const rel = '/' + to.path.slice(base.length)
      if (rel !== to.path) return { path: rel, query: to.query, hash: to.hash }
    }
  })
}
`)

const MIME = { '.png': 'image/png', '.pdf': 'application/pdf', '.pptx': 'application/vnd.openxmlformats-officedocument.presentationml.presentation' }
const EXPORT_MIME = { pdf: MIME['.pdf'], pptx: MIME['.pptx'] }
// Static SPA assets the built deck serves (Vite output). Content-type by ext.
const SPA_MIME = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.mjs': 'text/javascript',
  '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png',
  '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.gif': 'image/gif', '.webp': 'image/webp',
  '.avif': 'image/avif', '.ico': 'image/x-icon', '.woff': 'font/woff', '.woff2': 'font/woff2',
  '.ttf': 'font/ttf', '.otf': 'font/otf', '.map': 'application/json', '.txt': 'text/plain', '.wasm': 'application/wasm',
}

const hash = (s) => createHash('sha256').update(s).digest('hex').slice(0, 16)
// Every cached render/build is keyed on CACHE_EPOCH so it auto-invalidates when
// either the pipeline (RENDER_VERSION) OR the installed theme (THEME_VERSION)
// changes — a tahta bump rebuilds everything with no manual RENDER_VERSION bump.
// Combined with the markdown being in the key, ANY change to a deck's source
// (edit/MCP/sync/automation) or its look produces a new id → never serves stale.
const CACHE_EPOCH = `${RENDER_VERSION}|${THEME_VERSION}`
// Cache key folds in the full visual config so a variant/accent/lang change rerenders.
const cfgKey = (c) => `${pickVariant(c.variant)}|${c.accent || ''}|${c.lang || ''}|${c.logo || ''}|${c.logoInvert ? 1 : 0}`
const deckId = (md, cfg) => hash(`${CACHE_EPOCH}|${cfgKey(cfg)}|${md}`)
const RENDER = join(CACHE, 'd') // rendered frames/exports, one dir per deckId
const deckDir = (id) => join(RENDER, id)
const pickVariant = (v) => (v && VARIANTS.has(v) ? v : DEFAULT_VARIANT)

// ── cache GC ─────────────────────────────────────────────────────────────────
// Built SPAs (CACHE/spa) and rendered frames (CACHE/d) are content-addressed and
// never overwritten, so every deck edit OR theme bump orphans a dir; build-entry
// markdown piles up in WORK too. Left alone the cache grows unbounded. Cap the
// total (spa + d) size and evict the least-recently-served dirs. Correctness-safe:
// an evicted deck simply rebuilds on its next request (everything is recomputable).
const CACHE_MAX_MB = Number(process.env.DECK_CACHE_MAX_MB || 512)
const GC_INTERVAL_MS = Number(process.env.DECK_GC_INTERVAL_MS || 30 * 60_000)
const GC_MIN_AGE_MS = 5 * 60_000 // never reap a dir/entry younger than this (in-flight/just-built)

const lastUsed = new Map() // dir id -> ms of last serve (LRU signal; falls back to mtime)
const touch = (id) => { if (id) lastUsed.set(id, Date.now()) }

async function dirSize(p) {
  let total = 0
  let ents
  try { ents = await readdir(p, { withFileTypes: true }) } catch { return 0 }
  for (const e of ents) {
    const f = join(p, e.name)
    if (e.isDirectory()) total += await dirSize(f)
    else { try { total += statSync(f).size } catch {} }
  }
  return total
}

let gcRunning = false
async function gcSweep() {
  if (gcRunning) return
  gcRunning = true
  try {
    const now = Date.now()
    // 1) Reap stale build-entry markdown in WORK (one per build, never reused).
    //    Skip the setup/ dir (the shared router-guard) — only loose *.md files.
    try {
      for (const name of await readdir(WORK)) {
        if (!name.endsWith('.md')) continue
        const p = join(WORK, name)
        try { if (now - statSync(p).mtimeMs > GC_MIN_AGE_MS) await rm(p, { force: true }) } catch {}
      }
    } catch {}
    // 2) Cap total (spa + d) size, evicting least-recently-served dirs first.
    const entries = []
    let total = 0
    for (const root of [SPA, RENDER]) {
      if (!existsSync(root)) continue
      let names
      try { names = await readdir(root) } catch { continue }
      for (const name of names) {
        const p = join(root, name)
        let st
        try { st = statSync(p) } catch { continue }
        if (!st.isDirectory()) continue
        const size = await dirSize(p)
        entries.push({ p, id: name, size, mtimeMs: st.mtimeMs, used: lastUsed.get(name) ?? st.mtimeMs })
        total += size
      }
    }
    const cap = CACHE_MAX_MB * 1024 * 1024
    if (total <= cap) return
    entries.sort((a, b) => a.used - b.used) // least-recently-used first
    let freed = 0
    for (const e of entries) {
      if (total <= cap) break
      if (now - e.mtimeMs < GC_MIN_AGE_MS) continue // don't reap a fresh/in-flight build
      try {
        await rm(e.p, { recursive: true, force: true })
        lastUsed.delete(e.id)
        total -= e.size
        freed += e.size
      } catch {}
    }
    if (freed > 0) console.log(`deck cache gc: freed ${(freed / 1048576).toFixed(0)}MB, now ${(total / 1048576).toFixed(0)}MB (cap ${CACHE_MAX_MB}MB)`)
  } finally {
    gcRunning = false
  }
}

// The deck render config tela controls per-deck — the only inputs to the theme.
const reqConfig = (params) => ({
  variant: params.get('variant') || '',
  accent: params.get('accent') || '',
  lang: params.get('lang') || '',
  logo: params.get('logo') || '',
  logoInvert: params.get('logoInvert') === '1',
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
  if (cfg.logo) tc.logo = cfg.logo
  if (cfg.logoInvert) tc.logoInvert = true
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
  if (tc.logo) lines.push(`  logo: ${JSON.stringify(tc.logo)}`)
  if (tc.logoInvert) lines.push(`  logoInvert: true`)
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
  // Static frames for export + the MCP preview_deck tool (one per click-step).
  return {
    id,
    variant: pickVariant(cfg.variant),
    count: pngs.length,
    slides: pngs.map((f) => `/d/${id}/${f}`),
  }
}

// Render ONLY the first slide — the deck's "cover" (index thumbnail, public reader
// hero, OG share image). Much cheaper than a full render: one frame, no clicks.
// Cached + content-addressed under d/ like everything else (so the GC + public
// /d/ serving already apply). Keyed separately ('cover') from the full render.
const coverId = (md, cfg) => hash(`${CACHE_EPOCH}|${cfgKey(cfg)}|cover|${md}`)
async function renderCover(md, cfg) {
  const id = coverId(md, cfg)
  const dir = deckDir(id)
  if (!(await listFrames(dir)).length) {
    await mkdir(dir, { recursive: true })
    const entry = join(WORK, `cover-${id}.md`)
    await writeFile(entry, withTheme(await parseDeck(md), cfg))
    const release = await acquire()
    try {
      // Slide 1 only (no --with-clicks → final state of the cover slide).
      await exec(SLIDEV, ['export', entry, '--format', 'png', '--output', dir, '--range', '1', '--timeout', '60000'], {
        cwd: ROOT, timeout: 120_000, maxBuffer: 1 << 24,
      })
    } finally {
      release()
    }
  }
  const pngs = (await listFrames(dir)).sort(frameCmp)
  return { url: pngs[0] ? `/d/${id}/${pngs[0]}` : null }
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
  touch(id)
  res.writeHead(200, { 'content-type': MIME[extname(file)] || 'application/octet-stream', 'cache-control': 'public, max-age=31536000, immutable' })
  createReadStream(file).pipe(res)
}

// ── interactive SPA (live present) ───────────────────────────────────────────
// `slidev build` (pure Vite, no Chromium) → a self-contained static SPA with the
// real Slidev presenter/overview/drawing. `base` is the browser-facing path the
// backend serves it under (so asset URLs resolve); it's folded into the cache key
// because it's baked into the build output. tela gates access at its own layer.

const spaBuildId = (md, cfg, base) => hash(`${CACHE_EPOCH}|${cfgKey(cfg)}|${base}|${md}`)
const spaDir = (id) => join(SPA, id)
const inflightBuilds = new Map() // buildId -> Promise (dedupe the browser's parallel asset fetches)

async function buildSPA(md, cfg, base) {
  const id = spaBuildId(md, cfg, base)
  const dir = spaDir(id)
  if (existsSync(join(dir, 'index.html'))) return id
  if (inflightBuilds.has(id)) return inflightBuilds.get(id).then(() => id)
  const p = (async () => {
    const entry = join(WORK, `spa-${id}.md`)
    await writeFile(entry, withTheme(await parseDeck(md), cfg))
    const release = await acquire()
    try {
      await mkdir(dir, { recursive: true })
      await exec(SLIDEV, ['build', entry, '--base', base, '--out', dir], { cwd: ROOT, timeout: 300_000, maxBuffer: 1 << 24 })
    } finally {
      release()
    }
  })()
  inflightBuilds.set(id, p)
  try {
    await p
  } finally {
    inflightBuilds.delete(id)
  }
  return id
}

function serveSPA(res, id, name) {
  touch(id)
  const dir = spaDir(id)
  const rel = name || 'index.html'
  let file = normalize(join(dir, rel))
  if (file !== dir && !file.startsWith(dir + '/')) return void res.writeHead(404).end() // traversal guard
  if (!existsSync(file) || statSync(file).isDirectory()) {
    // History-mode SPA fallback: a client route (no file extension — e.g. "2",
    // "presenter", "presenter/1") serves index.html so the router can resolve it.
    // A missing real asset (has an extension) is a genuine 404.
    if (extname(rel) !== '') return void res.writeHead(404).end()
    file = join(dir, 'index.html')
    if (!existsSync(file)) return void res.writeHead(404).end()
  }
  const isIndex = file.endsWith('/index.html')
  res.writeHead(200, {
    'content-type': SPA_MIME[extname(file)] || 'application/octet-stream',
    // Vite content-hashes asset filenames → immutable; index.html must revalidate.
    'cache-control': isIndex ? 'no-cache' : 'public, max-age=31536000, immutable',
  })
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
      // tahta's own agent guide (AGENTS.md, verbatim) + the variant catalog, for
      // tela's MCP deck guide. Theme-owned so it never drifts as tahta grows.
      res.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'public, max-age=300' }).end(JSON.stringify(AUTHORING))
    } else if (req.method === 'POST' && path === '/lint') {
      // tahta's own structural validator (it owns the layout/field semantics).
      // Pass the RAW markdown — tahta's lint reparses it to see slide bodies + raw
      // YAML, which its body-level checks need (empty slide, duplicate YAML keys,
      // leaked/unclosed frontmatter). Passing only frontmatter objects silently
      // disables those. Slide numbers surfaced 1-based to match /parse + the outline.
      const md = await readBody(req)
      const r = await tahtaLint(md)
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
    } else if (req.method === 'POST' && path === '/cover') {
      // First-slide-only render → the deck's cover/OG image. count comes free from
      // the preflight parse (no extra work).
      const md = await readBody(req)
      const data = await preflight(md)
      const { url } = await renderCover(md, cfg) // resolve BEFORE writing headers
      res.writeHead(200, { 'content-type': 'application/json' }).end(JSON.stringify({ url, count: data.slides.length }))
    } else if (req.method === 'POST' && path.startsWith('/export/')) {
      const format = path.slice('/export/'.length)
      if (!EXPORT_MIME[format]) return void res.writeHead(400).end('format must be pdf or pptx')
      const md = await readBody(req)
      await preflight(md)
      const file = await renderFile(md, cfg, format)
      res.writeHead(200, { 'content-type': EXPORT_MIME[format] })
      createReadStream(file).pipe(res)
    } else if (req.method === 'POST' && path === '/spa') {
      // Build-if-needed + serve one file of the interactive SPA. tela's backend
      // proxies each browser asset GET here (with the deck body + the base it
      // serves under + the file). build is cached + in-flight-locked, so the
      // browser's parallel asset fetches don't double-build.
      const base = url.searchParams.get('base') || ''
      const name = url.searchParams.get('file') || 'index.html'
      if (!/^\/[\w./-]*\/$/.test(base)) return void res.writeHead(400).end('base must begin and end with /')
      const md = await readBody(req)
      const id = await buildSPA(md, cfg, base)
      serveSPA(res, id, name)
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

server.listen(PORT, () => console.log(`tela deck sidecar on :${PORT} — ${THEME_PKG} variants: [${[...VARIANTS]}], concurrency ${MAX_CONCURRENCY}, cache cap ${CACHE_MAX_MB}MB`))

// Cache GC: a sweep shortly after boot (reaps last run's orphans, e.g. after a
// theme bump) and then periodically. Best-effort; never blocks request serving.
setTimeout(() => { void gcSweep() }, 60_000)
setInterval(() => { void gcSweep() }, GC_INTERVAL_MS)
