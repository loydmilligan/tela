import { $command, $inputRule, $markSchema, $remark } from '@milkdown/kit/utils'
import { markRule } from '@milkdown/kit/prose'
import { toggleMark } from '@milkdown/kit/prose/commands'
import { highlightRemark } from '../../lib/markdown/transforms/highlight'

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
