import { describe, it, expect } from 'vitest'
import { unified } from 'unified'
import remarkParse from 'remark-parse'
import remarkDirective from 'remark-directive'
import type { Root } from 'mdast'
import {
  transformUnknownDirectivesInMdast,
  KNOWN_DIRECTIVE_NAMES,
  type MdastNode,
} from './unknown-directives'

// Parse markdown the way the editor does (remark-parse + remark-directive) so
// the transform is exercised against real directive nodes, then run the
// transform and report the resulting top-level node types.
function parse(md: string): Root {
  return unified().use(remarkParse).use(remarkDirective).parse(md) as Root
}
function topTypes(tree: Root): string[] {
  return (tree.children as unknown as MdastNode[]).map((n) => n.type)
}
function run(md: string): Root {
  const tree = parse(md)
  transformUnknownDirectivesInMdast(tree as unknown as MdastNode)
  return tree
}

describe('transformUnknownDirectivesInMdast', () => {
  it('drops an empty unknown container directive entirely (the :::diagram crash repro)', () => {
    const tree = run(':::diagram\n:::\n')
    // No directive node survives to reach Milkdown's strict parser.
    expect(topTypes(tree)).not.toContain('containerDirective')
    expect(topTypes(tree)).toEqual([])
  })

  it('unwraps an unknown container directive to its block children', () => {
    const tree = run(':::note\nhello world\n:::\n')
    const types = topTypes(tree)
    expect(types).not.toContain('containerDirective')
    expect(types).toEqual(['paragraph'])
    // content preserved
    const para = tree.children[0] as unknown as MdastNode
    const text = (para.children?.[0] as MdastNode | undefined)?.value
    expect(text).toBe('hello world')
  })

  it('leaves every KNOWN directive untouched', () => {
    for (const name of KNOWN_DIRECTIVE_NAMES) {
      const tree = run(`:::${name}\nbody\n:::\n`)
      expect(topTypes(tree)).toContain('containerDirective')
      expect((tree.children[0] as unknown as MdastNode).name).toBe(name)
    }
  })

  it('unwraps an unknown directive NESTED inside a known one (would re-crash via the known runner)', () => {
    const tree = run(':::tabs\n### Tab\n:::unknownnested\ninner\n:::\n:::\n')
    const tabs = tree.children[0] as unknown as MdastNode
    expect(tabs.type).toBe('containerDirective')
    expect(tabs.name).toBe('tabs')
    // No unknown directive anywhere in the subtree.
    const seen: string[] = []
    const walk = (n: MdastNode) => {
      if (n.type === 'containerDirective' || n.type === 'leafDirective' || n.type === 'textDirective') {
        seen.push(String(n.name))
      }
      n.children?.forEach(walk)
    }
    walk(tabs)
    expect(seen).toEqual(['tabs']) // only the known one remains
  })

  it('unwraps an unknown leaf directive into a paragraph (valid block flow)', () => {
    const tree = run('::callout[hi there]\n')
    const types = topTypes(tree)
    expect(types).not.toContain('leafDirective')
    expect(types).toContain('paragraph')
  })

  it('unwraps an unknown text directive inline, preserving surrounding text', () => {
    const tree = run('before :badge[X] after\n')
    const para = tree.children[0] as unknown as MdastNode
    const childTypes = (para.children ?? []).map((c) => c.type)
    expect(childTypes).not.toContain('textDirective')
    // the bracketed phrasing survives somewhere in the paragraph
    const flat = JSON.stringify(para)
    expect(flat).toContain('X')
    expect(flat).toContain('before ')
    expect(flat).toContain(' after')
  })
})
