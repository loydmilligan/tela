import { describe, it, expect } from 'vitest'
import { Schema } from '@milkdown/kit/prose/model'
import { EditorState, TextSelection } from '@milkdown/kit/prose/state'
import { buildInsertBlock } from './insert-block'

// A minimal schema that mirrors the structural shapes the real block inserters
// produce: a single-child container (callout / pull-quote), a two-child
// container whose body is the SECOND child (collapsible = summary + body), and
// a block atom (math / excalidraw-like). The helper's positioning logic is
// schema-agnostic, so exercising these three shapes proves it.
const schema = new Schema({
  nodes: {
    doc: { content: 'block+' },
    paragraph: {
      group: 'block',
      content: 'inline*',
      toDOM: () => ['p', 0],
      parseDOM: [{ tag: 'p' }],
    },
    // callout / pull-quote shape: body is the only child.
    callout: {
      group: 'block',
      content: 'paragraph+',
      toDOM: () => ['div', { class: 'callout' }, 0],
    },
    // collapsible shape: a summary textblock followed by a body paragraph.
    summary: { content: 'inline*', toDOM: () => ['summary', 0] },
    details: {
      group: 'block',
      content: 'summary paragraph',
      toDOM: () => ['details', 0],
    },
    // block atom (no editable content).
    rule: { group: 'block', atom: true, toDOM: () => ['hr'] },
    text: { group: 'inline' },
  },
})

const t = schema.nodes
const emptyCallout = () => t.callout.create(null, t.paragraph.create())
const emptyDetails = () =>
  t.details.create(null, [t.summary.create(), t.paragraph.create()])

// Build a state whose doc is `docNode`, with the caret at `pos` (defaults to a
// collapsed cursor at the start of the doc's first textblock).
function stateWith(docNode = schema.node('doc', null, [t.paragraph.create()])) {
  return EditorState.create({ schema, doc: docNode })
}

describe('buildInsertBlock', () => {
  it('lands the caret inside the body of a single-child scaffold (callout)', () => {
    const state = stateWith()
    const tr = buildInsertBlock(state, emptyCallout(), { caret: 'inside' })
    const next = state.apply(tr)
    const sel = next.selection
    expect(sel).toBeInstanceOf(TextSelection)
    // caret's textblock is the paragraph, whose parent is the callout.
    const { $from } = sel
    expect($from.parent.type).toBe(t.paragraph)
    expect($from.node($from.depth - 1).type).toBe(t.callout)
  })

  it('lands the caret in the BODY (second child) of a two-child scaffold (collapsible)', () => {
    const state = stateWith()
    const tr = buildInsertBlock(state, emptyDetails(), { caret: 'inside' })
    const next = state.apply(tr)
    const { $from } = next.selection
    // body is the paragraph child, NOT the summary.
    expect($from.parent.type).toBe(t.paragraph)
    expect($from.node($from.depth - 1).type).toBe(t.details)
    // and specifically the caret is in the details' LAST child.
    const details = $from.node($from.depth - 1)
    const indexInDetails = $from.index($from.depth - 1)
    expect(indexInDetails).toBe(details.childCount - 1)
  })

  it('targets the NEWLY inserted block, not a pre-existing identical one (the bug)', () => {
    // Doc already contains an empty callout; cursor sits in a trailing empty
    // paragraph AFTER it. The old content-matching walk picked the LAST empty
    // callout — which would be the pre-existing one once ours is also empty.
    const pre = emptyCallout()
    const trailing = t.paragraph.create()
    const doc = schema.node('doc', null, [pre, trailing])
    let state = EditorState.create({ schema, doc })
    // put the cursor in the trailing paragraph (after the pre-existing callout)
    const trailingStart = pre.nodeSize + 1
    state = state.apply(
      state.tr.setSelection(
        TextSelection.near(state.doc.resolve(trailingStart)),
      ),
    )

    const tr = buildInsertBlock(state, emptyCallout(), { caret: 'inside' })
    const next = state.apply(tr)

    // two callouts now exist.
    let count = 0
    let firstPos = -1
    next.doc.descendants((n, pos) => {
      if (n.type === t.callout) {
        count++
        if (firstPos < 0) firstPos = pos
      }
      return true
    })
    expect(count).toBe(2)

    // the caret must be inside the SECOND (newly inserted) callout — i.e. its
    // position is past the first callout, not inside it.
    const { $from } = next.selection
    const calloutPos = $from.before($from.depth - 1)
    expect(calloutPos).toBeGreaterThan(firstPos)
  })

  it('inserting two scaffolds in a row leaves two blocks with the caret in the second', () => {
    // Two top-level empty paragraphs — the realistic shape, since the slash menu
    // always fires from a paragraph (the one that held `/callout`). Insert into
    // the first, then move the caret to the second and insert again.
    const doc = schema.node('doc', null, [
      t.paragraph.create(),
      t.paragraph.create(),
    ])
    let state = EditorState.create({ schema, doc })
    state = state.apply(
      buildInsertBlock(state, emptyCallout(), { caret: 'inside' }),
    )
    // move the caret into the remaining trailing empty paragraph.
    const trailing = TextSelection.near(
      state.doc.resolve(state.doc.content.size - 1),
    )
    state = state.apply(state.tr.setSelection(trailing))
    state = state.apply(
      buildInsertBlock(state, emptyCallout(), { caret: 'inside' }),
    )

    let count = 0
    let lastPos = -1
    state.doc.descendants((n, pos) => {
      if (n.type === t.callout) {
        count++
        lastPos = pos
      }
      return true
    })
    expect(count).toBe(2)
    // caret is inside the last (newly inserted) callout.
    const { $from } = state.selection
    expect($from.before($from.depth - 1)).toBe(lastPos)
  })

  it('splits a non-empty paragraph and still lands inside the new block', () => {
    // cursor in the middle of "abcd"
    const para = t.paragraph.create(null, schema.text('abcd'))
    const doc = schema.node('doc', null, [para])
    let state = EditorState.create({ schema, doc })
    state = state.apply(
      state.tr.setSelection(TextSelection.near(state.doc.resolve(3))),
    )
    const tr = buildInsertBlock(state, emptyCallout(), { caret: 'inside' })
    const next = state.apply(tr)
    const { $from } = next.selection
    expect($from.parent.type).toBe(t.paragraph)
    expect($from.node($from.depth - 1).type).toBe(t.callout)
    // the original text survives split across paragraphs around the callout.
    expect(next.doc.textContent).toBe('abcd')
  })

  it("caret 'none' leaves ProseMirror's default selection (no throw, node inserted)", () => {
    const state = stateWith()
    const tr = buildInsertBlock(state, t.rule.create(), { caret: 'none' })
    const next = state.apply(tr)
    let hasRule = false
    next.doc.descendants((n) => {
      if (n.type === t.rule) hasRule = true
      return true
    })
    expect(hasRule).toBe(true)
  })
})
