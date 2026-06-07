import { $nodeSchema } from '@milkdown/kit/utils'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'

// M19 — timeline: a `:::timeline` container directive wrapping a bullet list of
// dated events. tela renders a vertical rail with a marker dot per event and
// the leading bold date set apart as a chip; in plain markdown it degrades to a
// readable bullet list of `**date** label — detail` lines. Round-trips via
// mdast-util-directive (directive + list), so nothing proprietary is stored and
// the events stay fully editable as ordinary list items.
//
// Pure schema + CSS — no nodeView. The rail, dots, and date-chip styling all
// key off the nested list in editor.css, so the events edit exactly like a
// normal list and the visual treatment follows for free.

interface MdastNode {
  type: string
  name?: string
  children?: MdastNode[]
}

export const timelineSchema = $nodeSchema('timeline', () => ({
  group: 'block',
  content: 'block+',
  defining: true,
  parseDOM: [{ tag: 'div[data-timeline]' }],
  toDOM: () => ['div', { 'data-timeline': '', class: 'tela-timeline' }, 0],
  parseMarkdown: {
    match: (node) =>
      node.type === 'containerDirective' &&
      (node as MdastNode).name === 'timeline',
    runner: (state, node, type) => {
      const paraType = type.schema.nodes.paragraph
      const children = (node as MdastNode).children ?? []
      state.openNode(type)
      if (children.length === 0 && paraType) {
        state.openNode(paraType)
        state.closeNode()
      } else {
        state.next(children as never)
      }
      state.closeNode()
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'timeline',
    runner: (state, node) => {
      state.openNode('containerDirective', undefined, { name: 'timeline' })
      state.next(node.content)
      state.closeNode()
    },
  },
}))

// Slash inserter: a `:::timeline` seeded with a few dated events. The leading
// bold date in each item renders as a chip; the em-dash splits label / detail.
export function insertTimeline(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const { schema } = view.state
  const timelineType = schema.nodes.timeline
  const listType = schema.nodes.bullet_list
  const itemType = schema.nodes.list_item
  const paraType = schema.nodes.paragraph
  const strongMark = schema.marks.strong
  if (!timelineType || !listType || !itemType || !paraType) return
  const mkItem = (date: string, rest: string) => {
    const dateText = strongMark
      ? schema.text(date, [strongMark.create()])
      : schema.text(date)
    const restText = schema.text(' ' + rest)
    return itemType.create(null, paraType.create(null, [dateText, restText]))
  }
  const list = listType.create(null, [
    mkItem('2026-01-15', 'v1.0 shipped — first stable release'),
    mkItem('2026-03-01', 'v1.1 — search + exports'),
    mkItem('2026-06-01', 'v2.0 — planned'),
  ])
  const node = timelineType.create(null, list)
  view.dispatch(view.state.tr.replaceSelectionWith(node).scrollIntoView())
  view.focus()
}
