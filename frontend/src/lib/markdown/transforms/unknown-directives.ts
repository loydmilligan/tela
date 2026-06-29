// Pure, Milkdown-free fallback for UNRECOGNIZED `:::name` / `::name` / `:name`
// directives. SINGLE SOURCE for the "degrade an unknown directive to its plain
// nested content" rule, mirroring the read-only view renderer's
// `containerDirective`/`leafDirective`/`textDirective` default branch in
// view/MarkdownView.tsx (render children, lose nothing).
//
// WHY THIS EXISTS: Milkdown's mdast→ProseMirror parser is strict — it throws
// `parserMatchError` ("Cannot match target parser for node") on ANY mdast node
// type it has no registered schema for, and that aborts the whole editor mount
// (blank, uneditable body). remark-directive happily parses `:::anything` into
// a directive node, but only the names with a dedicated editor schema (below)
// have a parser. A foreign/typo'd directive in a page body (sync import, paste,
// or an API/agent write) would otherwise hard-crash the editor. The read VIEW
// never crashed because it walks the tree itself and defaults unknown nodes to
// their children; this transform gives the EDIT path the same graceful
// degradation BEFORE the strict parser runs, keeping view≡edit.

export interface MdastNode {
  type: string
  name?: string
  children?: MdastNode[]
  [k: string]: unknown
}

// Directive names that DO have a dedicated Milkdown editor schema and so must
// survive to the parser untouched. MUST stay in sync with the editor
// `$nodeSchema` consumers whose `parseMarkdown.match` keys off
// `containerDirective` + name: milkdown-{pullquote(quote),tabs,kanban,
// stat-grid(stats),embed,file,calendar,timeline}.ts. A name dropped here would
// make that block unwrap to plain content in the editor — caught immediately
// when authoring the block (it stops rendering), the same coupling the view
// renderer's name switch already carries.
export const KNOWN_DIRECTIVE_NAMES = new Set<string>([
  'quote',
  'tabs',
  'kanban',
  'stats',
  'embed',
  'file',
  'calendar',
  'timeline',
])

const DIRECTIVE_TYPES = new Set<string>([
  'containerDirective',
  'leafDirective',
  'textDirective',
])

function isUnknownDirective(node: MdastNode): boolean {
  return (
    DIRECTIVE_TYPES.has(node.type) &&
    !KNOWN_DIRECTIVE_NAMES.has(typeof node.name === 'string' ? node.name : '')
  )
}

// Depth-first, rebuilding each parent's children so an unknown directive is
// replaced in place by its (already-transformed) children. Recurses into KNOWN
// directives too, so an unknown directive nested inside e.g. `:::tabs` is still
// unwrapped before that block's runner forwards the child to the strict parser.
export function transformUnknownDirectivesInMdast(node: MdastNode): void {
  if (!Array.isArray(node.children)) return
  const next: MdastNode[] = []
  for (const child of node.children) {
    transformUnknownDirectivesInMdast(child)
    if (isUnknownDirective(child)) {
      const kids = Array.isArray(child.children) ? child.children : []
      if (kids.length === 0) continue // empty directive → drops out entirely (view returns null)
      if (child.type === 'leafDirective') {
        // A leaf directive sits in block position but holds phrasing children;
        // wrap them in a paragraph so the unwrapped content is valid block flow.
        next.push({ type: 'paragraph', children: kids })
      } else {
        // Container (flow children) and text (phrasing, already inline) unwrap
        // straight into the parent's content.
        next.push(...kids)
      }
    } else {
      next.push(child)
    }
  }
  node.children = next
}

// Raw unified/remark attacher (mirrors calloutsRemark / collapsiblesRemark).
export function unknownDirectivesRemark() {
  return (tree: unknown) => {
    transformUnknownDirectivesInMdast(tree as unknown as MdastNode)
  }
}
