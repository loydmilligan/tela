import { $remark } from '@milkdown/kit/utils'
import { visit } from 'unist-util-visit'
import type { Link, Image } from 'mdast'

export function isSafeUrl(url: string): boolean {
  if (!url) return true
  if (url.startsWith('/') || url.startsWith('#') || url.startsWith('tela://')) return true
  try {
    const { protocol } = new URL(url)
    return /^https?:$/i.test(protocol) || protocol === 'mailto:'
  } catch {
    return true // relative URL — safe
  }
}

// Strips unsafe hrefs (javascript:, data:, etc.) from link and image nodes
// before they reach ProseMirror. Runs in the remark pipeline so neither
// editable nor read-only Milkdown can store or render a javascript: href.
export const remarkSafeLinks = $remark('remarkSafeLinks', () => () => (tree) => {
  visit(tree, ['link', 'image'], (node) => {
    const n = node as Link | Image
    if (!isSafeUrl(n.url)) n.url = '#'
  })
})
