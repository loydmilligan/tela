// Pure, Milkdown-free callout parsing + data. The SINGLE SOURCE shared by the
// Milkdown editor (milkdown-callouts.ts wraps `calloutsRemark` in `$remark` and
// builds the PM schema off this data) and the read-only view renderer's parser
// (lib/markdown/remark-stack.ts) + components. Keeping it Milkdown-free is what
// lets the view ship without dragging the editor in. See docs/view-edit-split.md.

export const CALLOUT_TYPES = [
  'note',
  'tip',
  'important',
  'warning',
  'caution',
] as const

export type CalloutType = (typeof CALLOUT_TYPES)[number]

export const CALLOUT_TYPE_SET = new Set<string>(CALLOUT_TYPES)

// Visible label rendered in the callout header. GitHub's published spec
// uppercases the keyword in rendered output ("NOTE", "TIP", …).
export const CALLOUT_LABELS: Record<CalloutType, string> = {
  note: 'Note',
  tip: 'Tip',
  important: 'Important',
  warning: 'Warning',
  caution: 'Caution',
}

// First-line marker. Anchored to `^` and the line break (or end-of-string for
// a marker-only blockquote like `> [!NOTE]`) so unrelated text that happens to
// contain `[!NOTE]` mid-paragraph does NOT trip the transform. The `i` flag
// keeps the user-facing UPPERCASE convention while tolerating lowercase
// markdown (GitHub itself accepts `[!note]`).
const ALERT_RE = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\](?:\r?\n|$)/i

export interface MdastNode {
  type: string
  value?: string
  children?: MdastNode[]
  calloutType?: CalloutType
  [k: string]: unknown
}

// Walk the mdast tree depth-first and rewrite qualifying blockquotes into
// callout nodes in place. We don't pull in `unist-util-visit` because the walk
// is trivial and the package adds ~3 KB raw to the bundle for one call site.
export function transformCalloutsInMdast(node: MdastNode): void {
  if (node.type === 'blockquote' && Array.isArray(node.children)) {
    const firstChild = node.children[0]
    if (
      firstChild?.type === 'paragraph' &&
      Array.isArray(firstChild.children)
    ) {
      const firstText = firstChild.children[0]
      if (firstText?.type === 'text' && typeof firstText.value === 'string') {
        const match = firstText.value.match(ALERT_RE)
        if (match) {
          const calloutType = match[1].toLowerCase() as CalloutType
          const stripped = firstText.value.slice(match[0].length)
          if (stripped.length === 0) {
            // Marker took up the entire first paragraph (common shape from
            // a re-parse of our own serializer's output, which emits the
            // marker as its own paragraph followed by a blank `>` line and
            // then the body paragraph). If there's a sibling paragraph
            // after it, drop the marker-only paragraph entirely and let
            // the next paragraph become the body root. Otherwise (truly
            // empty callout like `> [!NOTE]` standalone) drop the text
            // node but keep the empty paragraph so the schema's
            // `content: 'block+'` content matcher stays satisfied.
            if ((node.children?.length ?? 0) > 1) {
              node.children!.shift()
            } else {
              firstChild.children.shift()
            }
          } else {
            firstText.value = stripped
          }
          node.type = 'callout'
          node.calloutType = calloutType
        }
      }
    }
  }
  if (Array.isArray(node.children)) {
    for (const child of node.children) {
      transformCalloutsInMdast(child)
    }
  }
}

// Raw unified/remark attacher.
export function calloutsRemark() {
  return (tree: unknown) => {
    transformCalloutsInMdast(tree as unknown as MdastNode)
  }
}
