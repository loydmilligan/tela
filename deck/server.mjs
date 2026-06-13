// tela deck render sidecar — Slidev as a RENDER-ONLY service.
//
// A tela deck is a page whose body IS Slidev markdown. This service renders
// that markdown to static premium output, applying tela's own theme (the stored
// markdown never has to know the theme path). There is NO interactive SPA build
// and nothing Slidev runs in the viewer's browser — tela presents the rendered
// images in its own simple full-screen viewer. That keeps this service tiny.
//
//   POST /render            body: slidev markdown -> { id, count, slides:[url], pdf }
//   POST /export/<pdf|pptx> body: slidev markdown -> the file bytes
//   GET  /d/<id>/<file>     -> a rendered slide PNG / the PDF / the PPTX
//   GET  /health            -> ok
//
// Everything is cached by content hash (re-rendering an unchanged deck is
// instant). Runs as a compose sidecar (the gotenberg pattern); tela proxies it.

import http from 'node:http'
import { createHash } from 'node:crypto'
import { writeFile, mkdir, readdir } from 'node:fs/promises'
import { existsSync, createReadStream, statSync } from 'node:fs'
import { join, extname, dirname, basename, normalize } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'

const exec = promisify(execFile)
const ROOT = dirname(fileURLToPath(import.meta.url))
const THEME = join(ROOT, 'theme')
const CACHE = process.env.DECK_CACHE || join(ROOT, 'cache')
const WORK = join(CACHE, 'work')
const SLIDEV = join(ROOT, 'node_modules', '.bin', 'slidev')
const PORT = Number(process.env.PORT || 3344)

await mkdir(WORK, { recursive: true })

const MIME = { '.png': 'image/png', '.pdf': 'application/pdf', '.pptx': 'application/vnd.openxmlformats-officedocument.presentationml.presentation' }
const EXPORT_MIME = { pdf: MIME['.pdf'], pptx: MIME['.pptx'] }

const hash = (s) => createHash('sha256').update(s).digest('hex').slice(0, 16)
const deckDir = (id) => join(CACHE, 'd', id)

// Force tela's theme into the deck headmatter (override any user `theme:`), so
// the stored markdown stays portable and the look is controlled centrally.
function withTheme(md) {
  const fm = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/
  if (fm.test(md)) {
    return md.replace(fm, (_m, body) => {
      const kept = body.split('\n').filter((l) => !/^\s*theme\s*:/.test(l))
      return `---\ntheme: ${THEME}\n${kept.join('\n')}\n---\n`
    })
  }
  return `---\ntheme: ${THEME}\n---\n\n${md}`
}

async function slidevExport(md, format, outPath) {
  const entry = join(WORK, `${hash(md + format)}.md`)
  await writeFile(entry, withTheme(md))
  await exec(SLIDEV, ['export', entry, '--format', format, '--output', outPath, '--timeout', '60000'], {
    cwd: ROOT, timeout: 240_000, maxBuffer: 1 << 24,
  })
}

// Render to per-slide PNGs (the Present surface). Returns the manifest.
async function renderImages(md) {
  const id = hash(md)
  const dir = deckDir(id)
  if (!existsSync(join(dir, '1.png'))) {
    await mkdir(dir, { recursive: true })
    await slidevExport(md, 'png', dir) // -> 1.png, 2.png, ...
  }
  const pngs = (await readdir(dir))
    .filter((f) => /^\d+\.png$/.test(f))
    .sort((a, b) => parseInt(a) - parseInt(b))
  return {
    id,
    count: pngs.length,
    slides: pngs.map((f) => `/d/${id}/${f}`),
    pdf: `/export/pdf?id=${id}`,
  }
}

// Render a single downloadable file (cached alongside the images).
async function renderFile(md, format) {
  const file = join(deckDir(hash(md)), `deck.${format}`)
  if (!existsSync(file)) {
    await mkdir(dirname(file), { recursive: true })
    await slidevExport(md, format, file)
  }
  return file
}

function serveStatic(res, id, name) {
  const dir = deckDir(id)
  const file = normalize(join(dir, name))
  if (!file.startsWith(dir) || !existsSync(file) || statSync(file).isDirectory()) {
    return void res.writeHead(404).end()
  }
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

    if (req.method === 'POST' && path === '/render') {
      const manifest = await renderImages(await readBody(req))
      res.writeHead(200, { 'content-type': 'application/json' })
      res.end(JSON.stringify(manifest))
    } else if (req.method === 'POST' && path.startsWith('/export/')) {
      const format = path.slice('/export/'.length)
      if (!EXPORT_MIME[format]) return void res.writeHead(400).end('format must be pdf or pptx')
      const file = await renderFile(await readBody(req), format)
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

server.listen(PORT, () => console.log(`tela deck render sidecar on :${PORT} (theme: ${THEME})`))
