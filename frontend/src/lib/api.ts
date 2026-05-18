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
