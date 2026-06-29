import { $nodeSchema, $prose } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'
import { editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import { insertBlock } from '../../lib/milkdown/insert-block'

// Pull-quote: a `:::quote` container directive that renders as an elevated,
// large-type quotation with an optional attribution line. Round-trips through
// mdast-util-directive (same foundation as tabs/kanban) — the canonical form is
// `:::quote{cite="…"}` + block content, so nothing proprietary is stored and
// plain-markdown readers still see a directive they can degrade gracefully.
//
// Schema: `pullquote` (group block, content `block+`, attr `cite`). The quote
// body is the editable content hole; the attribution renders in a `<figcaption>`
// chrome element managed by the nodeView. In editable mode the caption is itself
// contenteditable and syncs back to the `cite` attr; in read mode it's static.

interface MdastNode {
  type: string
  name?: string
  attributes?: Record<string, string | null | undefined>
  children?: MdastNode[]
}

interface PullquoteAttrs {
  attrs: { cite: string }
}

export const pullquoteSchema = $nodeSchema('pullquote', () => ({
  group: 'block',
  content: 'block+',
  defining: true,
  attrs: {
    cite: { default: '', validate: 'string' },
  },
  parseDOM: [
    {
      tag: 'figure.tela-pullquote',
      getAttrs: (dom) => ({
        cite: dom instanceof HTMLElement ? (dom.dataset.cite ?? '') : '',
      }),
      contentElement: 'blockquote.tela-pullquote-body',
    },
  ],
  toDOM: (node) => {
    const { cite } = (node as unknown as PullquoteAttrs).attrs
    return [
      'figure',
      { class: 'tela-pullquote', 'data-cite': cite },
      ['blockquote', { class: 'tela-pullquote-body' }, 0],
    ]
  },
  parseMarkdown: {
    match: (node) =>
      node.type === 'containerDirective' && (node as MdastNode).name === 'quote',
    runner: (state, node, type) => {
      const cite = (node as MdastNode).attributes?.cite ?? ''
      state.openNode(type, { cite })
      const children = (node as MdastNode).children ?? []
      if (children.length > 0) {
        state.next(children as never)
      } else {
        // Empty directive → one empty paragraph so `block+` is satisfied.
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
    match: (node) => node.type.name === 'pullquote',
    runner: (state, node) => {
      const cite = (node.attrs.cite as string) || ''
      // Only emit `{cite="…"}` when there's an attribution to round-trip.
      const data: { name: string; attributes?: { cite: string } } = {
        name: 'quote',
      }
      if (cite) data.attributes = { cite }
      state.openNode('containerDirective', undefined, data)
      state.next(node.content)
      state.closeNode()
    },
  },
}))

// NodeView: adds the attribution `<figcaption>` as chrome below the quote body.
// Editable-mode caption is contenteditable and pushes edits back into the `cite`
// attr; read-mode (or empty) renders a static caption (or none).
export const pullquoteNodeView = $prose(() => {
  return new Plugin({
    props: {
      nodeViews: {
        pullquote: (node, view, getPos) => {
          const editable = view.editable
          const dom = document.createElement('figure')
          dom.className = 'tela-pullquote'

          const body = document.createElement('blockquote')
          body.className = 'tela-pullquote-body'

          const caption = document.createElement('figcaption')
          caption.className = 'tela-pullquote-cite'
          caption.setAttribute('contenteditable', editable ? 'true' : 'false')

          const renderCaption = (cite: string) => {
            dom.dataset.cite = cite
            // No attribution + not editable → hide the caption entirely so a
            // bare pull-quote doesn't leave a dangling empty line.
            if (!cite && !editable) {
              caption.style.display = 'none'
            } else {
              caption.style.display = ''
              if (document.activeElement !== caption) caption.textContent = cite
              caption.dataset.empty = cite ? 'false' : 'true'
            }
          }
          renderCaption((node.attrs.cite as string) || '')

          if (editable) {
            caption.dataset.placeholder = 'Attribution (optional)'
            caption.addEventListener('input', () => {
              const pos = typeof getPos === 'function' ? getPos() : null
              if (pos == null) return
              const value = caption.textContent ?? ''
              caption.dataset.empty = value ? 'false' : 'true'
              view.dispatch(view.state.tr.setNodeAttribute(pos, 'cite', value))
            })
            // Keep newlines out of the single-line attribution.
            caption.addEventListener('keydown', (e) => {
              if (e.key === 'Enter') e.preventDefault()
            })
          }

          dom.appendChild(body)
          dom.appendChild(caption)

          return {
            dom,
            contentDOM: body,
            update: (updated) => {
              if (updated.type !== node.type) return false
              renderCaption((updated.attrs.cite as string) || '')
              return true
            },
            ignoreMutation: (m) => {
              // The caption is ours; its edits sync through the input handler,
              // not PM reconciliation.
              if (caption.contains(m.target as Node)) return true
              return m.type === 'attributes' && m.target === dom
            },
          }
        },
      },
    },
  })
})

// Slash inserter: an empty pull-quote scaffold with the caret in the body.
export function insertPullquote(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const { schema } = view.state
  const pullquoteType = schema.nodes.pullquote
  const paraType = schema.nodes.paragraph
  if (!pullquoteType || !paraType) return
  const node = pullquoteType.create(null, paraType.create())
  insertBlock(view, node)
}
