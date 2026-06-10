import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DiagramSession, type DiagramSelf, type SceneElement } from './diagram-session'
import type { TelaProvider } from './tela-provider'

// A minimal stand-in for TelaProvider: records outbound ephemeral frames and
// lets a test inject inbound ones + simulate awareness removals. No ws, no Yjs.
class FakeProvider {
  sent: unknown[] = []
  private ephemeral = new Set<(p: Uint8Array) => void>()
  private awarenessChange = new Set<(c: { removed: number[] }) => void>()
  awareness: {
    clientID: number
    getLocalState: () => { user?: unknown } | null
    on: (ev: string, fn: (c: { removed: number[] }) => void) => void
    off: (ev: string, fn: (c: { removed: number[] }) => void) => void
  }

  constructor(clientID = 1) {
    this.awareness = {
      clientID,
      getLocalState: () => null,
      on: (_ev, fn) => this.awarenessChange.add(fn),
      off: (_ev, fn) => this.awarenessChange.delete(fn),
    }
  }

  sendEphemeral(payload: Uint8Array): void {
    this.sent.push(JSON.parse(new TextDecoder().decode(payload)))
  }
  onEphemeral(fn: (p: Uint8Array) => void): () => void {
    this.ephemeral.add(fn)
    return () => this.ephemeral.delete(fn)
  }

  // test helpers
  deliver(msg: unknown): void {
    const payload = new TextEncoder().encode(JSON.stringify(msg))
    for (const fn of this.ephemeral) fn(payload)
  }
  removeAwareness(clientID: number): void {
    for (const fn of this.awarenessChange) fn({ removed: [clientID] })
  }
  lastSent(): Record<string, unknown> {
    return this.sent[this.sent.length - 1] as Record<string, unknown>
  }
}

const KEY = 'diagram-xyz'
const SELF: DiagramSelf = { id: 7, username: 'me', colorIdx: 2, clientId: 1 }

function el(id: string, version: number): SceneElement {
  return { id, version, x: 0 }
}

function mk(provider: FakeProvider, self: DiagramSelf = SELF): DiagramSession {
  return new DiagramSession(provider as unknown as TelaProvider, KEY, self)
}

beforeEach(() => {
  vi.useFakeTimers()
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('DiagramSession', () => {
  it('requests current state on construction (late-join catchup)', () => {
    const p = new FakeProvider()
    mk(p)
    expect(p.sent[0]).toMatchObject({ k: KEY, t: 'r', u: { clientId: 1 } })
  })

  it('broadcasts only changed elements, diffed by version', () => {
    const p = new FakeProvider()
    const s = mk(p)
    p.sent.length = 0

    s.pushScene([el('a', 1), el('b', 1)])
    vi.advanceTimersByTime(60)
    expect(p.lastSent()).toMatchObject({ t: 's' })
    expect((p.lastSent().e as SceneElement[]).map((e) => e.id)).toEqual(['a', 'b'])

    // Re-push identical versions → nothing new goes out.
    p.sent.length = 0
    s.pushScene([el('a', 1), el('b', 1)])
    vi.advanceTimersByTime(60)
    expect(p.sent.length).toBe(0)

    // Bump one element → only that one is sent.
    s.pushScene([el('a', 1), el('b', 2)])
    vi.advanceTimersByTime(60)
    expect((p.lastSent().e as SceneElement[]).map((e) => e.id)).toEqual(['b'])

    s.destroy()
  })

  it('dispatches remote scene frames for our key, ignores other keys', () => {
    const p = new FakeProvider()
    const s = mk(p)
    const seen: SceneElement[][] = []
    s.onRemoteScene((els) => seen.push(els))

    p.deliver({ k: KEY, t: 's', e: [el('z', 3)], u: { id: 9, username: 'x', colorIdx: 0, clientId: 2 } })
    p.deliver({ k: 'other', t: 's', e: [el('q', 1)], u: { id: 9, username: 'x', colorIdx: 0, clientId: 2 } })

    expect(seen).toHaveLength(1)
    expect(seen[0][0].id).toBe('z')
    s.destroy()
  })

  it('answers a state request with the full cached scene', () => {
    const p = new FakeProvider()
    const s = mk(p)
    s.pushScene([el('a', 1), el('b', 1)])
    vi.advanceTimersByTime(60)
    p.sent.length = 0

    p.deliver({ k: KEY, t: 'r', u: { id: 9, username: 'x', colorIdx: 0, clientId: 2 } })
    expect(p.lastSent()).toMatchObject({ t: 's' })
    expect((p.lastSent().e as SceneElement[]).map((e) => e.id)).toEqual(['a', 'b'])
    s.destroy()
  })

  it('keys collaborators by clientId so two tabs of one user are distinct', () => {
    const p = new FakeProvider()
    const s = mk(p)
    const snapshots: Map<string, unknown>[] = []
    s.onCollaborators((c) => snapshots.push(c))

    p.deliver({ k: KEY, t: 'p', x: 1, y: 1, u: { id: 7, username: 'me', colorIdx: 2, clientId: 10 } })
    p.deliver({ k: KEY, t: 'p', x: 2, y: 2, u: { id: 7, username: 'me', colorIdx: 2, clientId: 11 } })

    const last = snapshots[snapshots.length - 1]
    expect(last.size).toBe(2)
    expect(last.has('10')).toBe(true)
    expect(last.has('11')).toBe(true)
    s.destroy()
  })

  it('removes a collaborator on an explicit leave', () => {
    const p = new FakeProvider()
    const s = mk(p)
    const snapshots: Map<string, unknown>[] = []
    s.onCollaborators((c) => snapshots.push(c))

    p.deliver({ k: KEY, t: 'p', x: 1, y: 1, u: { id: 7, username: 'me', colorIdx: 2, clientId: 10 } })
    p.deliver({ k: KEY, t: 'l', u: { id: 7, username: 'me', colorIdx: 2, clientId: 10 } })

    expect(snapshots[snapshots.length - 1].size).toBe(0)
    s.destroy()
  })

  it('removes a collaborator when its awareness entry is removed (hard close)', () => {
    const p = new FakeProvider()
    const s = mk(p)
    const snapshots: Map<string, unknown>[] = []
    s.onCollaborators((c) => snapshots.push(c))

    p.deliver({ k: KEY, t: 'p', x: 1, y: 1, u: { id: 7, username: 'me', colorIdx: 2, clientId: 10 } })
    p.removeAwareness(10)

    expect(snapshots[snapshots.length - 1].size).toBe(0)
    s.destroy()
  })

  it('elects the lowest clientId among active peers as checkpoint leader', () => {
    // We are clientId 5. Alone → leader.
    const p = new FakeProvider(5)
    const s = mk(p, { id: 7, username: 'me', colorIdx: 0, clientId: 5 })
    expect(s.isCheckpointLeader()).toBe(true)

    // A peer with a HIGHER clientId appears → still leader.
    p.deliver({ k: KEY, t: 'p', x: 0, y: 0, u: { id: 8, username: 'a', colorIdx: 0, clientId: 9 } })
    expect(s.isCheckpointLeader()).toBe(true)

    // A peer with a LOWER clientId appears → no longer leader.
    p.deliver({ k: KEY, t: 'p', x: 0, y: 0, u: { id: 8, username: 'b', colorIdx: 0, clientId: 2 } })
    expect(s.isCheckpointLeader()).toBe(false)
    s.destroy()
  })

  it('sends a leave frame on destroy', () => {
    const p = new FakeProvider()
    const s = mk(p)
    p.sent.length = 0
    s.destroy()
    expect(p.lastSent()).toMatchObject({ k: KEY, t: 'l' })
  })
})
