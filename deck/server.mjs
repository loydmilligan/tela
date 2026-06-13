// tela deck render sidecar — Slidev as a RENDER-ONLY service.
//
// A tela deck is a page whose body IS Slidev markdown. This service renders that
// markdown to static premium output (per-slide PNGs + PDF/PPTX), applying ONE OF
// tela's themes (chosen per deck via ?theme=). There is no interactive SPA and
// nothing Slidev runs in the viewer's browser — tela presents the rendered
// images in its own simple full-screen viewer. That keeps this service tiny.
//
//   GET  /themes                      -> [{ name, label, description }]   (for the editor's selector)
//   POST /render?theme=<name>         body: markdown -> { id, count, slides:[url], theme }
//   POST /export/<pdf|pptx>?theme=<name>  body: markdown -> the file bytes
//   GET  /d/<id>/<file>               -> a rendered slide PNG / the PDF / the PPTX
//   GET  /health                      -> ok
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

const exec = promisify(execFile)
const ROOT = dirname(fileURLToPath(import.meta.url))
const THEMES_DIR = join(ROOT, 'themes')
const CACHE = process.env.DECK_CACHE || join(ROOT, 'cache')
const WORK = join(CACHE, 'work')
const SLIDEV = join(ROOT, 'node_modules', '.bin', 'slidev')
const PORT = Number(process.env.PORT || 3344)
const MAX_CONCURRENCY = Number(process.env.DECK_CONCURRENCY || 2)

// Bump when the theme set or the render pipeline changes so cached decks rerender.
const RENDER_VERSION = 'r1'
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

// Inject the chosen theme into the deck headmatter (override any user `theme:`),
// so the stored markdown stays portable and the look is controlled centrally.
function withTheme(md, theme) {
  const path = join(THEMES_DIR, theme)
  const fm = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/
  if (fm.test(md)) {
    return md.replace(fm, (_m, body) => {
      const kept = body.split('\n').filter((l) => !/^\s*theme\s*:/.test(l))
      return `---\ntheme: ${path}\n${kept.join('\n')}\n---\n`
    })
  }
  return `---\ntheme: ${path}\n---\n\n${md}`
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
  await writeFile(entry, withTheme(md, theme))
  const release = await acquire()
  try {
    await exec(SLIDEV, ['export', entry, '--format', format, '--output', outPath, '--timeout', '60000'], {
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
  return { id, theme, count: pngs.length, slides: pngs.map((f) => `/d/${id}/${f}`) }
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
    } else if (req.method === 'POST' && path === '/render') {
      res.writeHead(200, { 'content-type': 'application/json' })
      res.end(JSON.stringify(await renderImages(await readBody(req), theme)))
    } else if (req.method === 'POST' && path.startsWith('/export/')) {
      const format = path.slice('/export/'.length)
      if (!EXPORT_MIME[format]) return void res.writeHead(400).end('format must be pdf or pptx')
      const file = await renderFile(await readBody(req), theme, format)
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
    res.writeHead(500, { 'content-type': 'text/plain' }).end(String(e?.stack || e))
  }
})

server.listen(PORT, () => console.log(`tela deck sidecar on :${PORT} — themes: [${[...THEMES]}], concurrency ${MAX_CONCURRENCY}`))
