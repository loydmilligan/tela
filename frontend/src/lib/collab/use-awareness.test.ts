import { describe, expect, it } from 'vitest'
import type { Awareness } from 'y-protocols/awareness'
import { snapshotDiagramEditors } from './use-awareness'

// Fake just the two members snapshotDiagramEditors reads.
function fakeAwareness(
  clientID: number,
  states: Array<[number, unknown]>,
): Awareness {
  return {
    clientID,
    getStates: () => new Map(states),
  } as unknown as Awareness
}

describe('snapshotDiagramEditors', () => {
  it('groups other clients by the diagram they are editing', () => {
    const a = fakeAwareness(1, [
      [1, { user: { username: 'me' }, editingDiagramId: 'D1' }], // self → excluded
      [2, { user: { username: 'alice' }, editingDiagramId: 'D1' }],
      [3, { user: { username: 'bob' }, editingDiagramId: 'D1' }],
      [4, { user: { username: 'carol' }, editingDiagramId: 'D2' }],
    ])
    const m = snapshotDiagramEditors(a)
    expect(m.get('D1')?.sort()).toEqual(['alice', 'bob'])
    expect(m.get('D2')).toEqual(['carol'])
  })

  it('ignores peers not editing a diagram and empty ids', () => {
    const a = fakeAwareness(1, [
      [2, { user: { username: 'alice' } }], // no editingDiagramId
      [3, { user: { username: 'bob' }, editingDiagramId: '' }], // empty → skip
      [4, { editingDiagramId: 'D1' }], // no username → falls back to "Someone"
    ])
    const m = snapshotDiagramEditors(a)
    expect(m.has('D1')).toBe(true)
    expect(m.get('D1')).toEqual(['Someone'])
    expect(m.size).toBe(1)
  })

  it('returns an empty map when nobody is editing', () => {
    const a = fakeAwareness(1, [[2, { user: { username: 'alice' } }]])
    expect(snapshotDiagramEditors(a).size).toBe(0)
  })
})
