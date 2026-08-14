import { describe, expect, it } from 'vitest'
import type { Awareness } from 'y-protocols/awareness'
import { computeIsLeader } from './use-leader-election'

// Remote peers get a `user` field (a fully-initialized session announces one);
// pass bare ids for that normal shape, or [id, state] pairs to model ghosts.
function fakeAwareness(
  clientID: number,
  ids: Array<number | [number, Record<string, unknown>]>,
): Awareness {
  return {
    clientID,
    getStates: () =>
      new Map(
        ids.map((e) =>
          Array.isArray(e) ? e : [e, e === clientID ? {} : { user: { id: e } }],
        ),
      ),
  } as unknown as Awareness
}

describe('computeIsLeader', () => {
  it('no awareness (non-collab) → not leader', () => {
    expect(computeIsLeader(null)).toBe(false)
  })

  it('alone in the room → leader', () => {
    expect(computeIsLeader(fakeAwareness(5, [5]))).toBe(true)
  })

  it('peer with higher clientID → still leader', () => {
    expect(computeIsLeader(fakeAwareness(5, [5, 9]))).toBe(true)
  })

  it('peer with lower clientID → not leader', () => {
    expect(computeIsLeader(fakeAwareness(5, [2, 5]))).toBe(false)
  })

  // The page-1257 data-loss shape: pagehide removed our local awareness entry
  // and the ws survived, so the entry was never re-seeded. The local peer MUST
  // still count itself as a leader candidate — otherwise every save is
  // silently gated off while the editor keeps accepting edits via Yjs.
  it('own entry missing from the map → still a candidate (leader when lowest)', () => {
    expect(computeIsLeader(fakeAwareness(5, [9]))).toBe(true)
    expect(computeIsLeader(fakeAwareness(5, [2]))).toBe(false)
  })

  it('empty map (unseeded / mid-reconnect) → leader, never save-dead', () => {
    expect(computeIsLeader(fakeAwareness(5, []))).toBe(true)
  })

  // The prod ghost (2026-08-14): a leaked provider kept renewing a bare `{}`
  // awareness state with a lower clientID and held leadership forever, saving
  // nothing. Peers without a `user` field must not compete.
  it('remote peer without a user field (ghost) cannot win leadership', () => {
    expect(computeIsLeader(fakeAwareness(5, [[2, {}], 5]))).toBe(true)
    expect(computeIsLeader(fakeAwareness(5, [[2, { cursor: {} }], 5]))).toBe(
      true,
    )
  })

  it('remote peer WITH a user field still wins when lowest', () => {
    expect(
      computeIsLeader(fakeAwareness(5, [[2, { user: { id: 9 } }], 5])),
    ).toBe(false)
  })
})
