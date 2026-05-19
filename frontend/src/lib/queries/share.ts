import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ApiErrorBody } from '../types'

// Public token-scoped queries for /share/{token} routes. Bypasses the `api()`
// helper deliberately — `api()` dispatches `tela:auth-required` on 401 and the
// global handler bounces the user to /login. In share-mode a 401 means
// "password required", not "logged out", so we mirror the {error, code}
// envelope parsing manually and surface the distinct states to the UI.

export const shareKeys = {
  all: ['share'] as const,
  root: (token: string) => [...shareKeys.all, 'root', token] as const,
  page: (token: string, pageId: number) =>
    [...shareKeys.all, 'page', token, pageId] as const,
  tree: (token: string) => [...shareKeys.all, 'tree', token] as const,
}

export interface SharePagePayload {
  id: number
  title: string
  body: string
  updated_at: string
}

export interface SharePublicMeta {
  token: string
  include_descendants: boolean
  has_password: boolean
  expires_at: string | null
}

export interface ShareRootResponse {
  share: SharePublicMeta
  page: SharePagePayload
}

export interface SharePageResponse {
  page: SharePagePayload
}

export interface SharePageNode {
  id: number
  title: string
  parent_id: number | null
  position: number
}

export interface ShareTreeResponse {
  pages: SharePageNode[]
}

// Distinct error kinds the UI branches on. `password_required` and
// `not_found` are the two states the components need to render specific
// surfaces for; `rate_limited` carries the Retry-After seconds for the
// countdown; everything else collapses to a generic error.
export type ShareErrorKind =
  | 'password_required'
  | 'not_found'
  | 'rate_limited'
  | 'invalid_password'
  | 'bad_request'
  | 'network'
  | 'http_error'

export class ShareError extends Error {
  readonly kind: ShareErrorKind
  readonly status: number
  readonly retryAfter: number | null
  constructor(
    kind: ShareErrorKind,
    status: number,
    message: string,
    retryAfter: number | null = null,
  ) {
    super(message)
    this.name = 'ShareError'
    this.kind = kind
    this.status = status
    this.retryAfter = retryAfter
  }
}

async function shareFetch<T>(path: string, init?: RequestInit): Promise<T> {
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
    res = await fetch(path, { ...init, headers })
  } catch (err) {
    throw new ShareError(
      'network',
      0,
      err instanceof Error ? err.message : 'network error',
    )
  }

  if (res.status === 204) {
    return undefined as T
  }

  const contentType = res.headers.get('Content-Type') ?? ''
  const isJson = contentType.includes('application/json')

  if (!res.ok) {
    let body: ApiErrorBody | null = null
    if (isJson) {
      body = (await res.json().catch(() => null)) as ApiErrorBody | null
    }
    const code = body?.code ?? 'http_error'
    const message = body?.error ?? `HTTP ${res.status}`
    if (res.status === 401 && code === 'password_required') {
      throw new ShareError('password_required', res.status, message)
    }
    if (res.status === 401) {
      // Wrong-password submissions return 401 password_required too, but auth
      // mutation callers translate the kind themselves. Reach here only for
      // anomalous 401s we don't know about — treat as invalid_password so the
      // password screen surfaces a generic "Incorrect password" rather than a
      // bare HTTP error.
      throw new ShareError('invalid_password', res.status, message)
    }
    if (res.status === 404) {
      throw new ShareError('not_found', res.status, message)
    }
    if (res.status === 429) {
      const retryHeader = res.headers.get('Retry-After')
      const retryAfter = retryHeader ? Number(retryHeader) : null
      throw new ShareError(
        'rate_limited',
        res.status,
        message,
        Number.isFinite(retryAfter) && retryAfter ? retryAfter : 60,
      )
    }
    if (res.status === 400) {
      throw new ShareError('bad_request', res.status, message)
    }
    throw new ShareError('http_error', res.status, message)
  }

  if (!isJson) {
    throw new ShareError(
      'http_error',
      res.status,
      `expected JSON, got "${contentType}"`,
    )
  }
  return (await res.json()) as T
}

export function useShareRoot(token: string) {
  return useQuery<ShareRootResponse, ShareError>({
    queryKey: shareKeys.root(token),
    queryFn: () =>
      shareFetch<ShareRootResponse>(`/api/share/${encodeURIComponent(token)}`),
    retry: false,
    staleTime: 30_000,
  })
}

export function useSharePage(token: string, pageId: number, enabled: boolean) {
  return useQuery<SharePageResponse, ShareError>({
    queryKey: shareKeys.page(token, pageId),
    queryFn: () =>
      shareFetch<SharePageResponse>(
        `/api/share/${encodeURIComponent(token)}/page/${pageId}`,
      ),
    retry: false,
    staleTime: 30_000,
    enabled,
  })
}

export function useShareTree(token: string, enabled: boolean) {
  return useQuery<ShareTreeResponse, ShareError>({
    queryKey: shareKeys.tree(token),
    queryFn: () =>
      shareFetch<ShareTreeResponse>(
        `/api/share/${encodeURIComponent(token)}/tree`,
      ),
    retry: false,
    staleTime: 30_000,
    enabled,
  })
}

export function useShareAuth(token: string) {
  const qc = useQueryClient()
  return useMutation<void, ShareError, string>({
    mutationFn: async (password: string) => {
      try {
        await shareFetch<{ ok: boolean }>(
          `/api/share/${encodeURIComponent(token)}/auth`,
          {
            method: 'POST',
            body: JSON.stringify({ password }),
          },
        )
      } catch (err) {
        // 401 on /auth carries `invalid_password` semantics regardless of the
        // backend's code value (it's `password_required` server-side). The
        // shareFetch translator above already mapped non-password_required
        // 401s to invalid_password; remap password_required 401s on the auth
        // surface here too so the UI doesn't need to know the difference.
        if (err instanceof ShareError && err.kind === 'password_required') {
          throw new ShareError('invalid_password', err.status, err.message)
        }
        throw err
      }
    },
    onSuccess: () => {
      // The root query was sitting in a `password_required` error state;
      // invalidating triggers a refetch that will now succeed with the
      // freshly minted cookie.
      void qc.invalidateQueries({ queryKey: shareKeys.root(token) })
      void qc.invalidateQueries({ queryKey: shareKeys.tree(token) })
    },
  })
}
