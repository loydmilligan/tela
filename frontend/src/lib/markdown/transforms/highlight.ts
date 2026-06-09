import { findAndReplace } from 'mdast-util-find-and-replace'

// Pure, Milkdown-free `==highlight==` parsing. SINGLE SOURCE shared by the
// Milkdown editor (milkdown-highlight.ts wraps this in `$remark` + builds the
// mark schema) and the view renderer's parser (lib/markdown/remark-stack.ts).
// See docs/view-edit-split.md.

interface MdastNodeLike {
  type: string
  children?: unknown[]
}

// Regular function (not arrow) so unified binds `this` to the processor,
// letting us register the stringify handler for our custom `highlight` node.
// Typed loosely — the unified/mdast generic types don't line up cleanly.
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
        state: {
          enter: (t: string) => () => void
          containerPhrasing: (n: unknown, info: unknown) => string
        },
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
