import type { ApiErrorBody } from './types'

// Same-origin: Caddy proxies /api/* to backend in prod; Vite dev proxy does the same.
const BASE = ''

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

// Dispatched when any non-auth API call comes back 401 — the session has
// expired or the user was deactivated mid-page. main.tsx subscribes once at
// module load and bounces the user to /login?next=<current>. We use a window
// event rather than importing the router directly to avoid an import cycle
// (router imports queries which import api).
const AUTH_REQUIRED_EVENT = 'tela:auth-required'

export interface AuthRequiredDetail {
  next: string
}

export function subscribeToAuthRequired(
  cb: (detail: AuthRequiredDetail) => void,
): () => void {
  function handler(e: Event) {
    cb((e as CustomEvent<AuthRequiredDetail>).detail)
  }
  window.addEventListener(AUTH_REQUIRED_EVENT, handler as EventListener)
  return () =>
    window.removeEventListener(AUTH_REQUIRED_EVENT, handler as EventListener)
}

function isAuthEndpoint(path: string): boolean {
  // /api/auth/me probing must NOT trigger the redirect — its 401 is the
  // canonical "no session" state, not a mid-session expiry.
  return path.startsWith('/api/auth/')
}

function emitAuthRequired(): void {
  const next = window.location.pathname + window.location.search
  window.dispatchEvent(
    new CustomEvent<AuthRequiredDetail>(AUTH_REQUIRED_EVENT, {
      detail: { next },
    }),
  )
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  const hasBody = init?.body !== undefined && init.body !== null
  if (hasBody && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  if (!headers.has('Accept')) {
    headers.set('Accept', 'application/json')
  }

  let res: Response
  try {
    res = await fetch(BASE + path, { ...init, headers })
  } catch (err) {
    throw new ApiError(0, 'network', err instanceof Error ? err.message : 'network error')
  }

  if (res.status === 204) {
    return undefined as T
  }

  const contentType = res.headers.get('Content-Type') ?? ''
  const isJson = contentType.includes('application/json')

  if (!res.ok) {
    if (res.status === 401 && !isAuthEndpoint(path)) {
      emitAuthRequired()
    }
    if (isJson) {
      const body = (await res.json().catch(() => null)) as ApiErrorBody | null
      if (body && typeof body.error === 'string' && typeof body.code === 'string') {
        throw new ApiError(res.status, body.code, body.error)
      }
    }
    const fallback = await res.text().catch(() => '')
    throw new ApiError(res.status, 'http_error', fallback || `HTTP ${res.status}`)
  }

  if (!isJson) {
    throw new ApiError(res.status, 'unexpected_content_type', `expected JSON, got "${contentType}"`)
  }
  return (await res.json()) as T
}

// Mirrors backend/internal/api/search.go's searchHit. `snippet` contains
// literal `<mark>…</mark>` delimiters around matched tokens; the body is NOT
// HTML-escaped server-side, so it must be rendered via HighlightedSnippet
// (split-and-emit) rather than dangerouslySetInnerHTML.
export interface SearchResult {
  page_id: number
  space_id: number
  title: string
  snippet: string
  breadcrumb: string[]
}

export function searchPages(
  q: string,
  signal?: AbortSignal,
): Promise<{ results: SearchResult[] }> {
  return api<{ results: SearchResult[] }>(
    `/api/search?q=${encodeURIComponent(q)}`,
    { signal },
  )
}

// A semantic (RAG) chunk hit — meaning-aware, chunk-level. heading_path is the
// section breadcrumb; chunk_id can feed a future read_chunk. The server returns
// 503 when no embedder is configured (feature dark) — callers treat that as "no
// smart results", not an error.
export interface SemanticHit {
  chunk_id: number
  page_id: number
  space_id: number
  title: string
  heading_path: string
  snippet: string
  score: number
  updated_at: string
}

export function searchSemantic(
  q: string,
  signal?: AbortSignal,
): Promise<{ results: SemanticHit[] }> {
  return api<{ results: SemanticHit[] }>(
    `/api/rag/search?q=${encodeURIComponent(q)}&mode=hybrid`,
    { signal },
  )
}

// "Ask your docs" — POST /api/rag/ask. The backend retrieves the top hybrid
// chunks (scoped to the caller's space_access, same anti-leak path as
// searchSemantic), grounds an LLM prompt on them, and returns a generated
// answer plus the cited source chunks (same shape as SemanticHit, minus the
// search-only score/heading framing — it IS rag.Hit). Sources may be empty when
// retrieval found nothing, in which case the answer says so.
//
// 503 codes to handle as a tasteful "unavailable" state (not an error toast):
//   rag_disabled — no embedder configured (TELA_RAG_EMBED_URL unset)
//   llm_disabled — no managed AI / completion model configured
export const ASK_UNAVAILABLE_CODES = ['rag_disabled', 'llm_disabled'] as const

export interface AskAnswer {
  answer: string
  sources: SemanticHit[]
}

export function askDocs(
  body: { question: string; space_id?: number },
  signal?: AbortSignal,
): Promise<AskAnswer> {
  return api<AskAnswer>('/api/rag/ask', {
    method: 'POST',
    body: JSON.stringify(body),
    signal,
  })
}
