import { $nodeSchema, $prose } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'
import {
  buildCalendarGrid,
  CALENDAR_EVENT_RE,
  pad2,
} from '../../lib/blocks/calendar-grid'

// M19 — calendar: a `:::calendar{month=YYYY-MM}` container directive over a
// bullet list of `YYYY-MM-DD Title` events. tela renders a month grid with the
// events placed on their days; in plain markdown it degrades to a readable,
// editable bullet list of dated lines. Round-trips via mdast-util-directive
// (directive + attribute + list), so the events stay the canonical source and
// nothing proprietary is stored.
//
// The nodeView renders the grid as non-editable chrome computed from the event
// list on every update (same source-plus-rendered idea as the mermaid block),
// and keeps the list itself as the editable contentDOM. In read-only mode the
// source list is hidden (CSS) so readers see only the grid; in edit mode the
// list shows below the grid so events can be added/changed. Single month per
// block, all-day events — matches the markdown-first, no-server-clock contract.
//
// The grid builder itself lives in lib/blocks/calendar-grid.ts (Milkdown-free)
// so the read-only view renderer (MarkdownView) renders the identical grid.

interface MdastNode {
  type: string
  name?: string
  attributes?: Record<string, string | null | undefined>
  children?: MdastNode[]
}

// Pull `{date -> [titles]}` from the calendar's event list. Each top-level
// list item whose text starts with an ISO date contributes one event on that
// day; non-conforming items are ignored (they still render in the editable
// source list, just not on the grid).
function collectEvents(node: ProseNode): Map<string, string[]> {
  const byDay = new Map<string, string[]>()
  node.descendants((n) => {
    if (n.type.name !== 'list_item') return true
    const line = n.textContent.trim().split('\n')[0]
    const m = CALENDAR_EVENT_RE.exec(line)
    if (m) {
      const list = byDay.get(m[1]) ?? []
      list.push(m[2].trim())
      byDay.set(m[1], list)
    }
    return false // don't descend into nested lists
  })
  return byDay
}

export const calendarSchema = $nodeSchema('calendar', () => ({
  group: 'block',
  content: 'block+',
  defining: true,
  attrs: { month: { default: '', validate: 'string' } },
  parseDOM: [
    {
      tag: 'div[data-calendar]',
      getAttrs: (dom) => ({
        month: dom instanceof HTMLElement ? (dom.dataset.month ?? '') : '',
      }),
      contentElement: 'div.tela-calendar-source',
    },
  ],
  toDOM: (node) => [
    'div',
    {
      'data-calendar': '',
      'data-month': node.attrs.month,
      class: 'tela-calendar',
    },
    ['div', { class: 'tela-calendar-source' }, 0],
  ],
  parseMarkdown: {
    match: (node) =>
      node.type === 'containerDirective' &&
      (node as MdastNode).name === 'calendar',
    runner: (state, node, type) => {
      const month = (node as MdastNode).attributes?.month ?? ''
      state.openNode(type, { month })
      const children = (node as MdastNode).children ?? []
      if (children.length > 0) {
        state.next(children as never)
      } else {
        const paraType = type.schema.nodes.paragraph
        if (paraType) {
          state.openNode(paraType)
          state.closeNode()
        }
      }
      state.closeNode()
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'calendar',
    runner: (state, node) => {
      const month = (node.attrs.month as string) || ''
      const data: { name: string; attributes?: { month: string } } = {
        name: 'calendar',
      }
      if (month) data.attributes = { month }
      state.openNode('containerDirective', undefined, data)
      state.next(node.content)
      state.closeNode()
    },
  },
}))

// NodeView: the rendered month grid (chrome, recomputed on update) above the
// editable source list (contentDOM). Read-only mode hides the source via CSS.
export const calendarNodeView = $prose(() => {
  return new Plugin({
    props: {
      nodeViews: {
        calendar: (node, view) => {
          const dom = document.createElement('div')
          dom.className = 'tela-calendar'
          dom.dataset.editable = view.editable ? 'true' : 'false'

          let grid = buildCalendarGrid(
            (node.attrs.month as string) || '',
            collectEvents(node),
          )
          dom.appendChild(grid)

          const source = document.createElement('div')
          source.className = 'tela-calendar-source'
          dom.appendChild(source)

          // Re-render the grid only when the month or the event set changes,
          // so typing inside an unrelated part of the list doesn't thrash.
          let lastKey = `${node.attrs.month}|${JSON.stringify([...collectEvents(node)])}`

          return {
            dom,
            contentDOM: source,
            update: (updated) => {
              if (updated.type !== node.type) return false
              dom.dataset.month = (updated.attrs.month as string) || ''
              const events = collectEvents(updated)
              const key = `${updated.attrs.month}|${JSON.stringify([...events])}`
              if (key !== lastKey) {
                lastKey = key
                const next = buildCalendarGrid(
                  (updated.attrs.month as string) || '',
                  events,
                )
                grid.replaceWith(next)
                grid = next
              }
              return true
            },
            ignoreMutation: (m) => {
              if (grid.contains(m.target as Node)) return true
              return m.type === 'attributes' && m.target === dom
            },
          }
        },
      },
    },
  })
})

// Slash inserter: a calendar for the current month seeded with one event.
export function insertCalendar(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const { schema } = view.state
  const calType = schema.nodes.calendar
  const listType = schema.nodes.bullet_list
  const itemType = schema.nodes.list_item
  const paraType = schema.nodes.paragraph
  if (!calType || !listType || !itemType || !paraType) return
  const now = new Date()
  const month = `${now.getUTCFullYear()}-${pad2(now.getUTCMonth() + 1)}`
  const today = now.toISOString().slice(0, 10)
  const item = itemType.create(
    null,
    paraType.create(null, schema.text(`${today} An event`)),
  )
  const node = calType.create({ month }, listType.create(null, [item]))
  view.dispatch(view.state.tr.replaceSelectionWith(node).scrollIntoView())
  view.focus()
}
