import { afterEach, describe, expect, it, vi } from 'vitest'

// client-errors.ts holds module-level dedup/cap state, so each test re-imports a
// fresh copy (vi.resetModules) after stubbing the browser globals it touches
// (window + fetch). The unit project runs in a node env, so neither exists by
// default.

type FetchMock = ReturnType<typeof vi.fn>

let listeners: Array<{ type: string; handler: EventListener; capture?: boolean }>
let fetchMock: FetchMock

async function freshModule(fetchImpl?: FetchMock) {
  vi.resetModules()
  listeners = []
  fetchMock =
    fetchImpl ?? vi.fn(() => Promise.resolve({ ok: true, status: 204 } as Response))
  ;(globalThis as Record<string, unknown>).fetch = fetchMock
  ;(globalThis as Record<string, unknown>).window = {
    location: { href: 'https://tela.test/p/7' },
    addEventListener: (type: string, handler: EventListener, capture?: boolean) =>
      listeners.push({ type, handler, capture }),
  }
  return import('./client-errors')
}

function lastBody(): Record<string, unknown> {
  const call = fetchMock.mock.calls.at(-1)
  return JSON.parse((call?.[1] as RequestInit).body as string)
}

afterEach(() => {
  delete (globalThis as Record<string, unknown>).window
  delete (globalThis as Record<string, unknown>).fetch
})

describe('reportClientError', () => {
  it('POSTs a well-formed beacon with the current URL', async () => {
    const m = await freshModule()
    m.reportClientError({ kind: 'collab', message: 'sync wedged', stack: 'at a (b.js:1)' })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/client-errors')
    expect((init as RequestInit).method).toBe('POST')
    expect((init as RequestInit).keepalive).toBe(true)
    const body = lastBody()
    expect(body).toMatchObject({
      kind: 'collab',
      message: 'sync wedged',
      stack: 'at a (b.js:1)',
      url: 'https://tela.test/p/7',
    })
  })

  it('attaches the ambient page id, and clears it', async () => {
    const m = await freshModule()
    m.setErrorReportPageId(42)
    m.reportClientError({ kind: 'error', message: 'one' })
    expect(lastBody().page_id).toBe(42)
    m.setErrorReportPageId(undefined)
    m.reportClientError({ kind: 'error', message: 'two' })
    expect(lastBody().page_id ?? null).toBeNull()
  })

  it('de-dupes identical reports but lets distinct ones through', async () => {
    const m = await freshModule()
    m.reportClientError({ kind: 'error', message: 'same', stack: 'at x' })
    m.reportClientError({ kind: 'error', message: 'same', stack: 'at x' })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    m.reportClientError({ kind: 'error', message: 'different', stack: 'at x' })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('caps the number of beacons per session', async () => {
    const m = await freshModule()
    for (let i = 0; i < 40; i++) {
      m.reportClientError({ kind: 'error', message: `msg-${i}` })
    }
    expect(fetchMock.mock.calls.length).toBe(25)
  })

  it('never throws when the beacon fetch rejects', async () => {
    const rejecting = vi.fn(() => Promise.reject(new Error('offline'))) as FetchMock
    const m = await freshModule(rejecting)
    expect(() => m.reportClientError({ kind: 'error', message: 'x' })).not.toThrow()
  })
})

describe('installGlobalErrorReporting', () => {
  it('wires uncaught, rejection, and resource listeners', async () => {
    const m = await freshModule()
    m.installGlobalErrorReporting()
    const types = listeners.map((l) => l.type)
    expect(types.filter((t) => t === 'error')).toHaveLength(2) // bubble + capture
    expect(types).toContain('unhandledrejection')
    // The resource listener must be the capture-phase one.
    expect(listeners.some((l) => l.type === 'error' && l.capture === true)).toBe(true)
  })

  it('reports a JS error from the bubble handler, ignores resource events there', async () => {
    const m = await freshModule()
    m.installGlobalErrorReporting()
    const bubble = listeners.find((l) => l.type === 'error' && !l.capture)!.handler
    bubble({ error: new Error('kaboom'), message: 'kaboom' } as unknown as Event)
    expect(lastBody()).toMatchObject({ kind: 'error', message: 'kaboom' })

    // A resource error (no .error / .message) must NOT be reported by the bubble
    // handler — it carries no JS exception.
    fetchMock.mockClear()
    bubble({} as Event)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('reports a failed asset load from the capture handler, skips window target', async () => {
    const m = await freshModule()
    m.installGlobalErrorReporting()
    const capture = listeners.find((l) => l.type === 'error' && l.capture)!.handler
    capture({ target: { tagName: 'IMG', src: 'https://x/a.png' } } as unknown as Event)
    expect(lastBody()).toMatchObject({
      kind: 'resource',
      message: 'failed to load img: https://x/a.png',
    })

    // A genuine JS error reaches the capture phase too (target is window) — the
    // capture handler must leave it to the bubble handler, not double-report.
    fetchMock.mockClear()
    capture({ target: (globalThis as Record<string, unknown>).window } as unknown as Event)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('suppresses a stale-chunk 404 and reloads once onto the fresh build', async () => {
    const reload = vi.fn()
    const store: Record<string, string> = {}
    const m = await freshModule()
    // The handlers read `window` at call time, so stub the extra surfaces
    // (reload + sessionStorage) the recovery path needs after freshModule set
    // its baseline window.
    ;(globalThis as Record<string, unknown>).window = {
      location: { href: 'https://tela.test/spaces/22/pages/326/x', reload },
      sessionStorage: {
        getItem: (k: string) => store[k] ?? null,
        setItem: (k: string, v: string) => {
          store[k] = v
        },
      },
      addEventListener: (type: string, handler: EventListener, capture?: boolean) =>
        listeners.push({ type, handler, capture }),
    }
    m.installGlobalErrorReporting()
    const capture = listeners.find((l) => l.type === 'error' && l.capture)!.handler

    // A same-origin hashed build asset 404 — expected post-deploy churn.
    capture({
      target: { tagName: 'LINK', href: 'https://tela.test/assets/HomeView-DJuVUy5M.js' },
    } as unknown as Event)
    expect(fetchMock).not.toHaveBeenCalled() // not reported as an error
    expect(reload).toHaveBeenCalledTimes(1) // recovered by reloading

    // A second stale-chunk 404 in the same 10s window must NOT reload again
    // (guard against a reload loop when a chunk is genuinely gone).
    capture({
      target: { tagName: 'LINK', href: 'https://tela.test/assets/compass-ByCIuQDY.js' },
    } as unknown as Event)
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('reports an unhandled rejection', async () => {
    const m = await freshModule()
    m.installGlobalErrorReporting()
    const onRej = listeners.find((l) => l.type === 'unhandledrejection')!.handler
    onRej({ reason: new Error('promise died') } as unknown as Event)
    expect(lastBody()).toMatchObject({ kind: 'unhandledrejection', message: 'promise died' })
  })
})
