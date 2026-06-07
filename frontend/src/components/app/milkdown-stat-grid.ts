import { $nodeSchema, $prose } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'

// M19 — stat grid: a `:::stats` container directive whose `### Label` sections
// become KPI tiles (label + a big value, with an optional trend). tela renders
// a responsive grid of tiles; in plain markdown it degrades to sequential
// headed sections (all readable, in order). Round-trips via mdast-util-directive
// — the canonical form IS the directive + headings, so nothing proprietary is
// stored.
//
// Schema: `stat_grid` (content `stat_tile+`) > `stat_tile` (content `block+`,
// attr `label`). The grouping (headings → tiles) happens in the parse runner;
// the inverse (tiles → directive + headings) in the serialize runner — exactly
// the tabs/kanban pattern. The nodeView adds the tile chrome: the label is
// editable in edit mode (synced to the attr) and a static caption in read-only
// mode; the tile accent is derived from a trend glyph (↑/↓ or +/-) in the value
// so `**$4.2M** ↑ 18%` tints green and `↓ 3%` tints red with no extra syntax.

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

// Trend accent inferred from the rendered value text. `↑`/`▲`/`+` ⇒ positive,
// `↓`/`▼`/`−`/`-` ⇒ negative, otherwise default (no tint). Keeps the markdown
// clean — the author writes a natural value line, the accent follows the glyph.
function accentForValue(text: string): 'positive' | 'negative' | 'default' {
  if (/[↑▲]|(?:^|\s)\+\d/.test(text)) return 'positive'
  if (/[↓▼−]|(?:^|\s)-\d/.test(text)) return 'negative'
  return 'default'
}

export const statTileSchema = $nodeSchema('stat_tile', () => ({
  group: 'stat_tile',
  content: 'block+',
  defining: true,
  attrs: { label: { default: '' } },
  parseDOM: [
    {
      tag: 'div[data-stat-tile]',
      getAttrs: (dom) => ({
        label: dom instanceof HTMLElement ? (dom.dataset.label ?? '') : '',
      }),
    },
  ],
  toDOM: (node) => [
    'div',
    { 'data-stat-tile': '', 'data-label': node.attrs.label, class: 'tela-stat' },
    0,
  ],
  // Produced/consumed by the parent `stat_grid` runner — no standalone markdown.
  parseMarkdown: { match: () => false, runner: () => {} },
  toMarkdown: {
    match: (node) => node.type.name === 'stat_tile',
    runner: (state, node) => {
      state.next(node.content)
    },
  },
}))

export const statGridSchema = $nodeSchema('stat_grid', () => ({
  group: 'block',
  content: 'stat_tile+',
  defining: true,
  parseDOM: [{ tag: 'div[data-stat-grid]' }],
  toDOM: () => ['div', { 'data-stat-grid': '', class: 'tela-stats' }, 0],
  parseMarkdown: {
    match: (node) =>
      node.type === 'containerDirective' &&
      (node as MdastNode).name === 'stats',
    runner: (state, node, type) => {
      const tileType = type.schema.nodes.stat_tile
      const paraType = type.schema.nodes.paragraph
      const children = (node as MdastNode).children ?? []
      state.openNode(type)
      let open = false
      let hasBlock = false
      const fillEmpty = () => {
        if (open && !hasBlock && paraType) {
          state.openNode(paraType)
          state.closeNode()
        }
      }
      for (const child of children) {
        if (child.type === 'heading' && child.depth === 3) {
          if (open) {
            fillEmpty()
            state.closeNode()
          }
          state.openNode(tileType, { label: headingText(child) })
          open = true
          hasBlock = false
        } else {
          if (!open) {
            state.openNode(tileType, { label: '' })
            open = true
            hasBlock = false
          }
          state.next(child as never)
          hasBlock = true
        }
      }
      if (open) {
        fillEmpty()
        state.closeNode()
      } else if (paraType) {
        state.openNode(tileType, { label: '' })
        state.openNode(paraType)
        state.closeNode()
        state.closeNode()
      }
      state.closeNode()
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'stat_grid',
    runner: (state, node) => {
      state.openNode('containerDirective', undefined, { name: 'stats' })
      node.forEach((tile) => {
        state.openNode('heading', undefined, { depth: 3 })
        state.addNode('text', undefined, (tile.attrs.label as string) || '')
        state.closeNode()
        state.next(tile.content)
      })
      state.closeNode()
    },
  },
}))

// NodeView: tile chrome. The label is an editable input synced to the attr in
// edit mode (a static caption in read-only). The accent (a top rail + value
// colour) follows the trend glyph in the value text, recomputed on each update.
export const statGridNodeViews = $prose(() => {
  return new Plugin({
    props: {
      nodeViews: {
        stat_tile: (node, view, getPos) => {
          const dom = document.createElement('div')
          dom.className = 'tela-stat'
          dom.dataset.accent = accentForValue(node.textContent)

          const head = document.createElement('div')
          head.className = 'tela-stat-head'
          head.setAttribute('contenteditable', 'false')

          let labelInput: HTMLInputElement | null = null
          if (view.editable) {
            const input = document.createElement('input')
            input.className = 'tela-stat-label tela-stat-label-input'
            input.value = (node.attrs.label as string) || ''
            input.placeholder = 'Label'
            input.spellcheck = false
            // Commit on blur/enter (not per keystroke) to keep undo history sane.
            const commit = () => {
              const pos = getPos()
              if (pos == null) return
              const cur = view.state.doc.nodeAt(pos)
              if (!cur || cur.type.name !== 'stat_tile') return
              if ((cur.attrs.label as string) === input.value) return
              view.dispatch(
                view.state.tr.setNodeMarkup(pos, undefined, {
                  ...cur.attrs,
                  label: input.value,
                }),
              )
            }
            input.addEventListener('blur', commit)
            input.addEventListener('keydown', (e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                input.blur()
              }
            })
            head.appendChild(input)
            labelInput = input
          } else {
            const span = document.createElement('span')
            span.className = 'tela-stat-label'
            span.textContent = (node.attrs.label as string) || ''
            head.appendChild(span)
          }

          const value = document.createElement('div')
          value.className = 'tela-stat-value'

          dom.appendChild(head)
          dom.appendChild(value)

          return {
            dom,
            contentDOM: value,
            update: (updated) => {
              if (updated.type !== node.type) return false
              dom.dataset.accent = accentForValue(updated.textContent)
              const label = (updated.attrs.label as string) || ''
              if (labelInput) {
                if (document.activeElement !== labelInput)
                  labelInput.value = label
              } else {
                const span = head.querySelector('.tela-stat-label')
                if (span) span.textContent = label
              }
              return true
            },
            ignoreMutation: (m) => {
              if (head.contains(m.target as Node)) return true
              return m.type === 'attributes' && m.target === dom
            },
          }
        },
      },
    },
  })
})

// Slash inserter: a three-tile scaffold with sample values.
export function insertStatGrid(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const { schema } = view.state
  const gridType = schema.nodes.stat_grid
  const tileType = schema.nodes.stat_tile
  const paraType = schema.nodes.paragraph
  if (!gridType || !tileType || !paraType) return
  const mkTile = (label: string, value: string) =>
    tileType.create({ label }, paraType.create(null, schema.text(value)))
  const node = gridType.create(null, [
    mkTile('Revenue', '$4.2M ↑ 18%'),
    mkTile('Active users', '12,400 ↑ 3%'),
    mkTile('Churn', '1.8% ↓ 0.4%'),
  ])
  view.dispatch(view.state.tr.replaceSelectionWith(node).scrollIntoView())
  view.focus()
}
