import { $nodeSchema, $prose } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import type { EditorView } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'

// Kanban board: a `:::kanban` container whose `### Column` sections each hold a
// checklist — rendered as a draggable board in tela, degrading to plain
// headings + checklists in markdown (so it round-trips losslessly; dragging a
// card just moves a `- [ ]` line under a different `### Column`). Built on the
// remark-directive foundation, same shape as tabs.
//
// Schema: `kanban` (content `kanban_column+`) > `kanban_column` (attr title,
// content `bullet_list`) > the stock list_item cards. The board view and
// cross-column drag are UI; the document is just the directive + headings +
// lists.

interface MdastNode {
  type: string
  depth?: number
  name?: string
  value?: string
  children?: MdastNode[]
}

function headingText(node: MdastNode): string {
  const parts: string[] = []
  const walk = (n: MdastNode) => {
    if (typeof n.value === 'string') parts.push(n.value)
    n.children?.forEach(walk)
  }
  node.children?.forEach(walk)
  return parts.join('').trim()
}

export const kanbanColumnSchema = $nodeSchema('kanban_column', () => ({
  group: 'kanban_column',
  content: 'bullet_list',
  defining: true,
  attrs: { title: { default: 'Column' } },
  parseDOM: [
    {
      tag: 'div[data-kanban-column]',
      getAttrs: (dom) => ({
        title:
          dom instanceof HTMLElement ? (dom.dataset.title ?? 'Column') : 'Column',
      }),
    },
  ],
  toDOM: (node) => [
    'div',
    {
      'data-kanban-column': '',
      'data-title': node.attrs.title,
      class: 'tela-kanban-col',
    },
    0,
  ],
  parseMarkdown: { match: () => false, runner: () => {} },
  toMarkdown: {
    match: (node) => node.type.name === 'kanban_column',
    runner: (state, node) => {
      state.next(node.content)
    },
  },
}))

export const kanbanSchema = $nodeSchema('kanban', () => ({
  group: 'block',
  content: 'kanban_column+',
  defining: true,
  parseDOM: [{ tag: 'div[data-kanban]' }],
  toDOM: () => ['div', { 'data-kanban': '', class: 'tela-kanban' }, 0],
  parseMarkdown: {
    match: (node) =>
      node.type === 'containerDirective' &&
      (node as MdastNode).name === 'kanban',
    runner: (state, node, type) => {
      const colType = type.schema.nodes.kanban_column
      const listType = type.schema.nodes.bullet_list
      const itemType = type.schema.nodes.list_item
      const paraType = type.schema.nodes.paragraph
      const children = (node as MdastNode).children ?? []

      const openEmptyList = () => {
        state.openNode(listType)
        state.openNode(itemType)
        state.openNode(paraType)
        state.closeNode()
        state.closeNode()
        state.closeNode()
      }

      state.openNode(type)
      let columnCount = 0
      let i = 0
      while (i < children.length) {
        const child = children[i]
        if (child.type === 'heading' && child.depth === 3) {
          state.openNode(colType, { title: headingText(child) || 'Column' })
          const next = children[i + 1]
          if (next && next.type === 'list') {
            state.next(next as never)
            i += 2
          } else {
            openEmptyList()
            i += 1
          }
          state.closeNode()
          columnCount += 1
        } else {
          // Stray content outside a column heading — skip (kanban markdown is
          // headings + lists by construction).
          i += 1
        }
      }
      if (columnCount === 0) {
        // No headings → one empty column so `kanban_column+` is satisfied.
        state.openNode(colType, { title: 'Column' })
        openEmptyList()
        state.closeNode()
      }
      state.closeNode()
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'kanban',
    runner: (state, node) => {
      state.openNode('containerDirective', undefined, { name: 'kanban' })
      node.forEach((column) => {
        state.openNode('heading', undefined, { depth: 3 })
        state.addNode('text', undefined, (column.attrs.title as string) || 'Column')
        state.closeNode()
        state.next(column.content)
      })
      state.closeNode()
    },
  },
}))

// ---- Card drag (cross-column move) -----------------------------------------
// Source list_item position, captured on dragstart. Module-scoped because the
// dragstart and drop fire on different column nodeViews.
let dragSource: { pos: number; size: number } | null = null

function listItemPosAt(view: EditorView, el: HTMLElement): { pos: number; size: number } | null {
  const docPos = view.posAtDOM(el, 0)
  if (docPos < 0) return null
  const $pos = view.state.doc.resolve(docPos)
  for (let d = $pos.depth; d >= 0; d--) {
    if ($pos.node(d).type.name === 'list_item') {
      const pos = $pos.before(d)
      return { pos, size: $pos.node(d).nodeSize }
    }
  }
  return null
}

// Insert position at the end of a column's bullet_list content.
function columnListEnd(colNode: ProseNode, colPos: number): number | null {
  const list = colNode.firstChild
  if (!list || list.type.name !== 'bullet_list') return null
  // colPos + 1 enters the column; +1 enters the list; + content size = end.
  return colPos + 2 + list.content.size
}

export const kanbanNodeViews = $prose(() => {
  return new Plugin({
    props: {
      nodeViews: {
        kanban: () => {
          const dom = document.createElement('div')
          dom.className = 'tela-kanban'
          return { dom, contentDOM: dom }
        },
        kanban_column: (node, view, getPos) => {
          let current = node
          const dom = document.createElement('div')
          dom.className = 'tela-kanban-col'

          const header = document.createElement('div')
          header.className = 'tela-kanban-col-header'
          header.setAttribute('contenteditable', 'false')
          const titleInput = document.createElement('input')
          titleInput.className = 'tela-kanban-col-title'
          titleInput.value = current.attrs.title as string
          titleInput.addEventListener('input', () => {
            const pos = getPos()
            if (pos == null) return
            view.dispatch(
              view.state.tr.setNodeMarkup(pos, undefined, {
                ...current.attrs,
                title: titleInput.value,
              }),
            )
          })
          header.appendChild(titleInput)

          const body = document.createElement('div')
          body.className = 'tela-kanban-col-body'

          // Mark card list items draggable + wire DnD (delegated on the body).
          const applyDraggable = () => {
            body.querySelectorAll<HTMLElement>(':scope > ul > li').forEach((li) => {
              li.setAttribute('draggable', 'true')
            })
          }
          body.addEventListener('dragstart', (e) => {
            const li = (e.target as HTMLElement)?.closest('li')
            if (!li || !body.contains(li)) return
            dragSource = listItemPosAt(view, li)
            e.dataTransfer?.setData('text/plain', 'card')
          })
          body.addEventListener('dragover', (e) => {
            if (dragSource) e.preventDefault() // allow drop
          })
          body.addEventListener('drop', (e) => {
            if (!dragSource) return
            e.preventDefault()
            const src = dragSource
            dragSource = null
            const pos = getPos()
            if (pos == null) return
            const colNode = view.state.doc.nodeAt(pos)
            if (!colNode) return
            const target = columnListEnd(colNode, pos)
            if (target == null) return
            const node2 = view.state.doc.nodeAt(src.pos)
            if (!node2 || node2.type.name !== 'list_item') return
            // Move = delete source, then insert at the (mapped) target. PM
            // validates the transaction, so a bad position just no-ops.
            try {
              let tr = view.state.tr.delete(src.pos, src.pos + src.size)
              const insertAt = tr.mapping.map(target)
              tr = tr.insert(insertAt, node2)
              view.dispatch(tr)
            } catch {
              /* invalid move — leave the doc untouched */
            }
          })

          dom.appendChild(header)
          dom.appendChild(body)
          requestAnimationFrame(applyDraggable)

          return {
            dom,
            contentDOM: body,
            update: (updated) => {
              if (updated.type !== current.type) return false
              current = updated
              if (titleInput.value !== updated.attrs.title) {
                titleInput.value = updated.attrs.title as string
              }
              requestAnimationFrame(applyDraggable)
              return true
            },
            ignoreMutation: (m) => {
              if (header.contains(m.target as Node)) return true
              // draggable attr toggles on cards are ours, not content edits.
              return m.type === 'attributes' && (m.target as HTMLElement)?.tagName === 'LI'
            },
          }
        },
      },
    },
  })
})

// Slash inserter: a three-column board, each with one starter card.
export function insertKanban(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const { schema } = view.state
  const kanbanType = schema.nodes.kanban
  const colType = schema.nodes.kanban_column
  const listType = schema.nodes.bullet_list
  const itemType = schema.nodes.list_item
  const paraType = schema.nodes.paragraph
  if (!kanbanType || !colType || !listType || !itemType || !paraType) return
  const mkColumn = (title: string, card: string) =>
    colType.create({ title }, listType.create(null, itemType.create(null, paraType.create(null, schema.text(card)))))
  const node = kanbanType.create(null, [
    mkColumn('To do', 'First task'),
    mkColumn('In progress', 'Second task'),
    mkColumn('Done', 'Third task'),
  ])
  view.dispatch(view.state.tr.replaceSelectionWith(node).scrollIntoView())
  view.focus()
}
