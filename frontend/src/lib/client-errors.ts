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
}
