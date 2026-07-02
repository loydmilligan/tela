// Browser-side error reporting. Crashes in a real user's tab (uncaught
// exceptions, unhandled promise rejections, React render errors) used to die
// silently in their console — invisible to us. This beacons them to
// POST /api/client-errors, where they land in the admin Events feed + a
// Prometheus counter. See backend/internal/api/client_errors.go.
//
// Best-effort by design: a failed report is swallowed (never re-reported — that
// would loop), and the whole path is wrapped so the reporter can never itself
// throw into the code that called it.

export type ClientErrorKind =
  | 'error'
  | 'unhandledrejection'
  | 'react'
  | 'collab'
  | 'resource'
  | 'query'
  | 'mutation'

export interface ClientErrorReport {
  kind: ClientErrorKind
  message: string
  stack?: string
  component?: string
  pageId?: number
}

// Per-session caps so an error loop (a render that throws every frame, a
// retrying layer) can't fire thousands of beacons. The server throttles too,
// but stopping it client-side avoids the wasted requests entirely.
const SESSION_CAP = 25
const seen = new Set<string>()
let sent = 0

// Stale-chunk recovery, shared with main.tsx's `vite:preloadError` handler.
// After a frontend redeploy the content-hashed asset filenames rotate and the
// old files are deleted server-side; a tab still running the old build 404s the
// moment it asks for a now-gone chunk. Reload once to pick up the fresh
// index.html + hashes. A 10s guard prevents a reload loop if a chunk is
// genuinely missing (bad deploy) rather than merely stale — after one failed
// reload we stop and let the error surface.
const CHUNK_RELOAD_KEY = 'tela:chunk-reload-at'
export function reloadOnceForStaleChunk(): void {
  try {
    const last = Number(sessionStorage.getItem(CHUNK_RELOAD_KEY) || 0)
    if (Date.now() - last > 10_000) {
      sessionStorage.setItem(CHUNK_RELOAD_KEY, String(Date.now()))
      window.location.reload()
    }
  } catch {
    // sessionStorage/reload can throw (disabled storage, private mode); the
    // recovery is best-effort and must never propagate into a caller.
  }
}

// A same-origin hashed build asset (/assets/<name>-<hash>.js|css). A 404 on one
// of these is expected churn right after a deploy (the old hash was deleted),
// NOT a real error — so we recover from it instead of reporting it as noise.
function isStaleChunkUrl(url: string): boolean {
  try {
    const here = new URL(window.location.href)
    const u = new URL(url, here)
    return u.origin === here.origin && /^\/assets\/.+\.(js|mjs|css)$/.test(u.pathname)
  } catch {
    return false
  }
}

// Ambient context the page can set so a report knows which page the user was
// on without the global handlers having to parse the router. Best-effort.
let currentPageId: number | undefined
export function setErrorReportPageId(id: number | undefined): void {
  currentPageId = id
}

function signature(r: ClientErrorReport): string {
  // First stack line is enough to collapse the same error firing repeatedly
  // while still distinguishing genuinely different failures.
  const firstFrame = (r.stack ?? '').split('\n', 2)[1]?.trim() ?? ''
  return `${r.kind}|${r.message}|${firstFrame}`
}

export function reportClientError(report: ClientErrorReport): void {
  try {
    if (sent >= SESSION_CAP) return
    const sig = signature(report)
    if (seen.has(sig)) return
    seen.add(sig)
    sent++

    const body = JSON.stringify({
      kind: report.kind,
      message: report.message,
      stack: report.stack,
      component: report.component,
      url: window.location.href,
      page_id: report.pageId ?? currentPageId,
    })
    // keepalive lets the POST survive a tab navigating/closing right after a
    // crash. credentials default to same-origin, so the session cookie rides
    // along; a 401 (not logged in) is fine — it just won't be recorded.
    void fetch('/api/client-errors', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body,
      keepalive: true,
    }).catch(() => {
      // Swallow — reporting the reporter's own failure would loop.
    })
  } catch {
    // Never let reporting throw into the caller.
  }
}

// Pull a message + stack out of whatever a handler hands us (Error, string,
// DOM ErrorEvent.error, a rejected non-Error value).
function extract(value: unknown): { message: string; stack?: string } {
  if (value instanceof Error) {
    return { message: value.message || value.name || 'Error', stack: value.stack }
  }
  if (typeof value === 'string') return { message: value }
  try {
    return { message: JSON.stringify(value) }
  } catch {
    return { message: String(value) }
  }
}

let installed = false

// Wire the global handlers once, at app startup (main.tsx). Idempotent.
export function installGlobalErrorReporting(): void {
  if (installed) return
  installed = true

  window.addEventListener('error', (e: ErrorEvent) => {
    // Resource-load errors (img/script 404) also fire 'error' but carry no
    // .error and bubble from the element — skip those; they're not JS crashes.
    if (!e.error && !e.message) return
    const { message, stack } = extract(e.error ?? e.message)
    reportClientError({ kind: 'error', message, stack })
  })

  window.addEventListener('unhandledrejection', (e: PromiseRejectionEvent) => {
    const { message, stack } = extract(e.reason)
    reportClientError({ kind: 'unhandledrejection', message, stack })
  })

  // Failed resource loads (a broken <script>/<link>/<img> — e.g. a stale lazy
  // chunk or a dead image URL). These fire 'error' on the element and do NOT
  // bubble, so they're only reachable in the capture phase, and they carry no
  // .error (the bubble-phase handler above filters them out). e.target is the
  // element here; for a genuine JS error it's window, which we skip.
  window.addEventListener(
    'error',
    (e: Event) => {
      const t = e.target as (HTMLElement & { src?: string; href?: string }) | null
      if (!t || !t.tagName) return
      const url = t.src || t.href || ''
      // A stale hashed chunk 404 (old <script>/<link> after a redeploy) is
      // expected, not a crash: don't report it (it would just be feed noise and
      // scare an admin into thinking pages vanished), and recover by reloading
      // once onto the fresh build so the user isn't stuck on a dead route.
      if (isStaleChunkUrl(url)) {
        reloadOnceForStaleChunk()
        return
      }
      reportClientError({
        kind: 'resource',
        message: `failed to load ${t.tagName.toLowerCase()}${url ? `: ${url}` : ''}`,
      })
    },
    true,
  )
}
