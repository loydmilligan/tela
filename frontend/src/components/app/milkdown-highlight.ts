import { $command, $inputRule, $markSchema, $remark } from '@milkdown/kit/utils'
import { markRule } from '@milkdown/kit/prose'
import { toggleMark } from '@milkdown/kit/prose/commands'
import { findAndReplace } from 'mdast-util-find-and-replace'

// Highlight mark: `==text==` → <mark>. A widely-supported markdown extension
// (Obsidian et al.), so it round-trips as plain text. `==` isn't CommonMark,
// so — like the callout/excalidraw transforms — we wire it by hand:
//   - parse: a remark transform (mdast-util-find-and-replace) splits `==x==`
//     in text nodes into a `highlight` mdast node;
//   - serialize: a to-markdown handler on the same remark plugin re-emits
//     `==x==` (remark-stringify can't render an unknown node otherwise);
//   - the mark schema maps the `highlight` mdast node ⇄ a PM mark;
//   - an input rule converts `==x==` as you type; a command + bubble-toolbar
//     button toggle it on a selection.

interface MdastNodeLike {
  type: string
  children?: unknown[]
}

// Regular function (not arrow) so unified binds `this` to the processor,
// letting us register the stringify handler for our custom `highlight` node.
// Typed loosely + cast at the $remark boundary — the unified/mdast generic
// types don't line up cleanly across the kit's re-exports.
// Exported as the SINGLE SOURCE of `==highlight==` parsing, shared by both the
// Milkdown editor ($remark wrapper below) and the standalone view parser
// (lib/markdown/remark-stack.ts). See docs/view-edit-split.md.
export function highlightRemark(this: { data: () => Record<string, unknown> }) {
  const data = this.data()
    const toMarkdownExtensions = (data.toMarkdownExtensions ||
      (data.toMarkdownExtensions = [])) as Array<{
      handlers: Record<string, unknown>
    }>
    toMarkdownExtensions.push({
      handlers: {
        highlight: (
          node: MdastNodeLike,
          _parent: unknown,
          state: { enter: (t: string) => () => void; containerPhrasing: (n: unknown, info: unknown) => string },
          info: unknown,
        ) => {
          const exit = state.enter('highlight')
          const value = state.containerPhrasing(node, {
            ...(info as object),
            before: '=',
            after: '=',
          })
          exit()
          return `==${value}==`
        },
      },
    })
  return (tree: unknown) => {
    findAndReplace(tree as never, [
      [
        /==([^=\n]+)==/g,
        (_full: string, inner: string) =>
          ({
            type: 'highlight',
            children: [{ type: 'text', value: inner }],
          }) as never,
      ],
    ])
  }
}

export const highlightRemarkPlugin = $remark(
  'telaHighlight',
  () => highlightRemark as never,
)

export const highlightSchema = $markSchema('highlight', () => ({
  parseDOM: [{ tag: 'mark' }],
  toDOM: () => ['mark', { class: 'tela-highlight' }, 0],
  parseMarkdown: {
    match: (node) => node.type === 'highlight',
    runner: (state, node, markType) => {
      state.openMark(markType)
      state.next(((node as { children?: unknown[] }).children ?? []) as never)
      state.closeMark(markType)
    },
  },
  toMarkdown: {
    match: (mark) => mark.type.name === 'highlight',
    runner: (state, mark) => {
      state.withMark(mark, 'highlight')
    },
  },
}))

export const toggleHighlightCommand = $command(
  'ToggleHighlight',
  (ctx) => () => toggleMark(highlightSchema.type(ctx)),
)

export const highlightInputRule = $inputRule((ctx) =>
  markRule(/==([^=\n]+)==/, highlightSchema.type(ctx)),
)
