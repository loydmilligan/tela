import { $inputRule, $nodeSchema, $remark } from '@milkdown/kit/utils'
import { editorViewCtx } from '@milkdown/kit/core'
import { InputRule } from '@milkdown/kit/prose/inputrules'
import { TextSelection } from '@milkdown/kit/prose/state'
import type { Ctx } from '@milkdown/ctx'
import { insertBlock } from '../../lib/milkdown/insert-block'
import {
  CALLOUT_LABELS,
  CALLOUT_TYPE_SET,
  CALLOUT_TYPES,
  type CalloutType,
  type MdastNode,
  calloutsRemark,
} from '../../lib/markdown/transforms/callouts'

// Callout parsing + data live in lib/markdown/transforms/callouts.ts (Milkdown-
// free, shared with the view renderer). This file keeps the editor-only pieces:
// the PM schema, the input rule, and the $remark wrapper. Re-export the shared
// data symbols so existing importers of this module keep working.
export { CALLOUT_LABELS, CALLOUT_TYPES, type CalloutType }

// M13.0 — GitHub-style blockquote alert callouts. Five GitHub-standard types
// (`> [!NOTE] / [!TIP] / [!IMPORTANT] / [!WARNING] / [!CAUTION]`) parse into a
// dedicated `callout` block node carrying a `type` attr; round-trip back to
// the same `> [!TYPE]\n> body` markdown shape on save. Unknown types fall
// through to plain blockquote (no callout chrome, no error).
//
// Three pieces wired together:
// 1. `calloutsRemarkPlugin` — mdast transformer that runs during parse. Walks
//    the tree post-`remark-parse`; matches `blockquote` whose first child is a
//    paragraph whose first text node starts with `[!TYPE]`, strips the marker,
//    and rewrites `node.type` to `'callout'` with `node.calloutType` carrying
//    the lowercased type. Serialization writes the marker back via the schema's
//    own `toMarkdown` runner — serialize does NOT re-run remark transformers
//    (only `remark.stringify(build())`), so the schema's toMarkdown must emit
//    a fully-formed mdast `blockquote` with the marker text prepended into
//    the first paragraph.
// 2. `calloutSchema` — `$nodeSchema('callout', ...)`. Block node, content
//    `block+`, attrs `{type: CalloutType}`. toDOM emits a 3-div structure:
//    outer host + non-editable header (icon + label) + editable body (the
//    `0` content hole). parseDOM allows copy-paste round-trip from rendered
//    HTML. parseMarkdown matches `type === 'callout'` (produced by the remark
//    transform above). toMarkdown emits mdast `blockquote` with the `[!TYPE]`
//    marker prepended to the first paragraph's text content.
// 3. `calloutInputRule` — live conversion as the user types. Fires on the
//    closing `]` of `[!TYPE]` when the cursor is inside a blockquote whose
//    sole child paragraph contains exactly `[!TYPE]`. Replaces the blockquote
//    with an empty callout of the matching type so the user can keep typing
//    body content without waiting for a save+reload round-trip.


// Mirror the `Element` interface used by PM's NodeSpec.toDOM. We type the
// argument loosely to dodge the cross-package Node-type mismatch between
// `@milkdown/prose/model`'s `Node` and the same type re-exported by
// `prosemirror-model` (they ARE the same class at runtime, but TS sees them
// as distinct under bundler module resolution).
interface CalloutSchemaNode {
  attrs: { type: CalloutType }
}

export const calloutsRemarkPlugin = $remark('telaCallouts', () => calloutsRemark)

export const calloutSchema = $nodeSchema('callout', () => ({
  content: 'block+',
  group: 'block',
  defining: true,
  attrs: {
    type: {
      default: 'note',
      validate: 'string',
    },
  },
  parseDOM: [
    {
      tag: 'div.tela-callout',
      getAttrs: (dom) => {
        const el = dom as HTMLElement
        const type = el.getAttribute('data-callout-type')
        if (!type || !CALLOUT_TYPE_SET.has(type)) return false
        return { type }
      },
      contentElement: 'div.tela-callout-body',
    },
  ],
  toDOM: (node) => {
    const { type } = (node as unknown as CalloutSchemaNode).attrs
    return [
      'div',
      {
        class: `tela-callout tela-callout-${type}`,
        'data-callout-type': type,
      },
      [
        'div',
        { class: 'tela-callout-header', contenteditable: 'false' },
        [
          'span',
          {
            class: 'tela-callout-icon',
            'data-callout-icon': type,
            'aria-hidden': 'true',
          },
        ],
        ['span', { class: 'tela-callout-label' }, CALLOUT_LABELS[type]],
      ],
      ['div', { class: 'tela-callout-body' }, 0],
    ]
  },
  parseMarkdown: {
    match: ({ type }) => type === 'callout',
    runner: (state, node, type) => {
      const calloutType =
        (node as MdastNode).calloutType &&
        CALLOUT_TYPE_SET.has((node as MdastNode).calloutType as string)
          ? ((node as MdastNode).calloutType as CalloutType)
          : 'note'
      state.openNode(type, { type: calloutType })
      state.next((node as MdastNode).children ?? [])
      state.closeNode()
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'callout',
    runner: (state, node) => {
      const calloutType = (node.attrs.type as CalloutType) ?? 'note'
      const marker = `[!${calloutType.toUpperCase()}]`
      state.openNode('blockquote')
      // Merge the marker into the first paragraph child as a `marker\n` text
      // prefix so the rendered markdown reads `> [!TYPE]\n> body` rather than
      // a separate marker-only blockquote line followed by a blank `>`. If
      // the first child isn't a paragraph (e.g., a list directly inside the
      // callout — unusual but allowed by `content: 'block+'`) emit a
      // stand-alone marker paragraph first to preserve the marker line.
      let isFirst = true
      node.content.forEach((child) => {
        if (isFirst) {
          isFirst = false
          if (child.type.name === 'paragraph') {
            state.openNode('paragraph')
            // Only emit the trailing newline if the paragraph actually has
            // body content. A marker-only callout (`> [!NOTE]` with empty
            // body, common immediately after the input rule fires) should
            // round-trip as `> [!NOTE]` — a trailing `\n` would render as
            // `> [!NOTE]\n> ` with a dangling blockquote prefix.
            const sep = child.content.size > 0 ? '\n' : ''
            state.addNode('text', undefined, marker + sep)
            state.next(child.content)
            state.closeNode()
            return
          }
          state.openNode('paragraph')
          state.addNode('text', undefined, marker)
          state.closeNode()
        }
        state.next(child)
      })
      // Empty callout (no children) still needs the marker to round-trip.
      if (isFirst) {
        state.openNode('paragraph')
        state.addNode('text', undefined, marker)
        state.closeNode()
      }
      state.closeNode()
    },
  },
}))

// Insert a fresh empty callout (default type `note`) at the cursor and land the
// caret inside its body paragraph — the slash-menu entry point, mirroring what
// the input rule produces when a user types `> [!NOTE]`. Replaces the selection
// so it behaves like the other block inserts.
export function insertCallout(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const { schema } = view.state
  const calloutType = schema.nodes.callout
  const paragraphType = schema.nodes.paragraph
  if (!calloutType || !paragraphType) return
  const callout = calloutType.create({ type: 'note' }, paragraphType.create())
  insertBlock(view, callout)
}

// Live conversion: user types `> [!NOTE]` (or any of the 5 types) on the
// first line of a brand-new blockquote → rewrites to a fresh empty callout so
// the user can keep typing the body inside the new chrome. Conservative
// trigger conditions: blockquote must contain exactly one paragraph whose
// entire text equals the marker. Pasted or pre-existing multi-paragraph
// blockquotes that happen to begin with `[!TYPE]` flow through the parser
// path on the next reload instead.
export const calloutInputRule = $inputRule(
  () =>
    new InputRule(
      /\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]$/i,
      (state, match, start) => {
        const { $from } = state.selection
        const blockDepth = $from.depth - 1
        if (blockDepth < 0) return null
        const wrapper = $from.node(blockDepth)
        if (wrapper.type.name !== 'blockquote') return null
        if (wrapper.childCount !== 1) return null
        if ($from.parent.type.name !== 'paragraph') return null
        // Match must span the entire paragraph content (no prefix text before
        // the marker). $from.start() returns the position immediately inside
        // the parent paragraph; if that equals the input-rule `start` then
        // the regex matched from the paragraph's first character.
        if ($from.start() !== start) return null
        const calloutType = match[1].toLowerCase() as CalloutType
        const calloutNodeType = state.schema.nodes.callout
        const paragraphNodeType = state.schema.nodes.paragraph
        if (!calloutNodeType || !paragraphNodeType) return null
        const blockStart = $from.before(blockDepth)
        const blockEnd = $from.after(blockDepth)
        const emptyPara = paragraphNodeType.create()
        const callout = calloutNodeType.create(
          { type: calloutType },
          emptyPara,
        )
        const tr = state.tr.replaceWith(blockStart, blockEnd, callout)
        // Land cursor inside the empty paragraph (callout open + paragraph
        // open = +2 from the block start position).
        tr.setSelection(TextSelection.create(tr.doc, blockStart + 2))
        return tr
      },
    ),
)
