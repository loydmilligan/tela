import { describe, expect, it } from 'vitest'
import type { Awareness } from 'y-protocols/awareness'
import { computeIsLeader } from './use-leader-election'

function fakeAwareness(clientID: number, ids: number[]): Awareness {
  return {
    clientID,
    getStates: () => new Map(ids.map((id) => [id, {}])),
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
})
