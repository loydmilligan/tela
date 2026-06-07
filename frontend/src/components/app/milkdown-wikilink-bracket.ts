import { $ctx, $nodeSchema, $prose, $remark } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'
import { findAndReplace } from 'mdast-util-find-and-replace'
import { pageSlug } from '../../lib/slug'
export { buildWikilinkResolveIndex } from '../../lib/slug'
import {
  wikilinkModeCtx,
  type WikilinkDecorationMode,
} from './milkdown-wikilink-decoration'

// Obsidian-style `[[Name]]` wikilinks — the bare-bracket sibling of the
// picker-inserted `[Title](tela://page/{id})` link (milkdown-wikilink.tsx).
// Agents and hand-written / synced markdown use `[[Name]]`; before this they
// rendered as inert text. We keep `[[Name]]` as the canonical on-disk form
// (round-trips untouched, so the vault/sync story stays clean) and resolve the
// name → page id only for RENDERING:
//
//   1. `wikilinkBracketRemarkPlugin` — a remark transform that rewrites
//      `[[Name]]` / `[[Name|Alias]]` / `[[Name#Heading]]` text into a custom
//      `wikilink` mdast node on parse, plus a to-markdown handler that re-emits
//      the exact `[[…]]` syntax on save (round-trip).
//   2. `wikilinkBracketSchema` — an inline atom PM node rendering an
//      `<a class="tela-wikilink" data-wikilink-slug="…">label</a>`. No href yet
//      — resolution is reactive (see below).
//   3. `wikilinkResolvePlugin` — a decoration plugin that looks each node's slug
//      up in `wikilinkResolveCtx` (a slug→id map pushed from React) and injects
//      `href="tela://page/{id}"` on the anchor when resolved, or a broken/
//      out-of-scope class when not. Injecting that href is the whole trick: the
//      existing modifier-click handler (editor) and the readers' click handlers
//      already navigate `tela://page/{id}` anchors, so bracket links light up
//      with zero new navigation code.
//
// Resolution mirrors the backend (pages.go resolveWikiTitleSlugs): slug-match a
// page title within the SAME space, lowest id wins — so what the editor renders
// as a live link is exactly what the backend records as a backlink.

// `[[Target]]`, `[[Target|Alias]]`, `[[Target#Heading]]`. Inner text has no
// nested brackets — mirrors the backend wikiBracketRE.
const BRACKET_RE = /\[\[([^[\]]+?)\]\]/g

interface WikilinkParts {
  target: string
  alias: string | null
}

// Split the inner text into target + optional display alias. The alias (after
// `|`) is display-only; a `#heading` suffix stays inside target (round-tripped,
// sliced off only for slug resolution).
function splitWikilink(inner: string): WikilinkParts {
  const bar = inner.indexOf('|')
  if (bar >= 0) {
    const alias = inner.slice(bar + 1).trim()
    return { target: inner.slice(0, bar).trim(), alias: alias.length > 0 ? alias : null }
  }
  return { target: inner.trim(), alias: null }
}

// Slug used for resolution: drop any `#heading`, then slugify the title exactly
// as the backend does (parity with pageSlug → resolveWikiTitleSlugs).
export function wikilinkSlug(target: string): string {
  const hash = target.indexOf('#')
  return pageSlug(hash >= 0 ? target.slice(0, hash) : target)
}

// ---- parse + serialize -----------------------------------------------------

interface MdastWikilink {
  type: 'wikilink'
  target: string
  alias: string | null
}

// Regular function so unified binds `this` to the processor, letting us register
// the to-markdown handler for our custom `wikilink` node (remark-stringify can't
// serialize an unknown node otherwise). Typed loosely + cast at the $remark
// boundary — the unified/mdast generics don't line up across the kit re-exports.
function wikilinkRemark(this: { data: () => Record<string, unknown> }) {
  const data = this.data()
  const toMarkdownExtensions = (data.toMarkdownExtensions ||
    (data.toMarkdownExtensions = [])) as Array<{ handlers: Record<string, unknown> }>
  toMarkdownExtensions.push({
    handlers: {
      wikilink: (node: MdastWikilink) =>
        node.alias ? `[[${node.target}|${node.alias}]]` : `[[${node.target}]]`,
    },
  })
  return (tree: unknown) => {
    findAndReplace(tree as never, [
      [
        BRACKET_RE,
        (_full: string, inner: string) => {
          const { target, alias } = splitWikilink(inner)
          // `[[ ]]` / `[[|x]]` — nothing to link; leave the literal text.
          if (target === '') return false as never
          return { type: 'wikilink', target, alias } as never
        },
      ],
    ])
  }
}

export const wikilinkBracketRemarkPlugin = $remark(
  'telaWikilinkBracket',
  () => wikilinkRemark as never,
)

interface WikilinkSchemaNode {
  attrs: { target: string; alias: string | null }
}

export const wikilinkBracketSchema = $nodeSchema('wikilink', () => ({
  group: 'inline',
  inline: true,
  atom: true,
  selectable: true,
  marks: '',
  attrs: {
    target: { default: '' },
    alias: { default: null },
  },
  parseDOM: [
    {
      tag: 'a[data-wikilink]',
      getAttrs: (dom) => {
        const el = dom as HTMLElement
        return {
          target: el.getAttribute('data-wikilink-target') ?? el.textContent ?? '',
          alias: el.getAttribute('data-wikilink-alias') || null,
        }
      },
    },
  ],
  toDOM: (node) => {
    const { target, alias } = (node as unknown as WikilinkSchemaNode).attrs
    const attrs: Record<string, string> = {
      'data-wikilink': 'true',
      'data-wikilink-slug': wikilinkSlug(target),
      'data-wikilink-target': target,
      class: 'tela-wikilink',
    }
    if (alias) attrs['data-wikilink-alias'] = alias
    return ['a', attrs, alias ?? target]
  },
  parseMarkdown: {
    match: ({ type }) => type === 'wikilink',
    runner: (state, node, type) => {
      const n = node as unknown as MdastWikilink
      state.addNode(type, {
        target: typeof n.target === 'string' ? n.target : '',
        alias: typeof n.alias === 'string' ? n.alias : null,
      })
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'wikilink',
    runner: (state, node) => {
      state.addNode('wikilink', undefined, undefined, {
        target: node.attrs.target,
        alias: node.attrs.alias,
      })
    },
  },
}))

// ---- resolution (reactive decoration) --------------------------------------

// Slug→id map for the active page's space. `null` = the React side hasn't pushed
// a snapshot yet → render nothing extra (links stay neutral, not redlined) until
// it lands. Mirrors the wikilinkAliveIdsCtx "don't know yet" convention.
export const wikilinkResolveCtx = $ctx<Map<string, number> | null, 'wikilinkResolve'>(
  null,
  'wikilinkResolve',
)

// Dispatched by the React side after swapping the slice so the plugin rebuilds
// without waiting for the next keystroke (same mechanism as the alive-ids meta).
export const WIKILINK_RESOLVE_META = 'tela-wikilink-resolve'

interface ResolveState {
  decos: DecorationSet
}

function buildResolveDecorations(
  doc: ProseNode,
  index: Map<string, number> | null,
  mode: WikilinkDecorationMode,
): DecorationSet {
  if (index == null) return DecorationSet.empty
  const decos: Decoration[] = []
  doc.descendants((node, pos) => {
    if (node.type.name !== 'wikilink') return
    const slug = wikilinkSlug(node.attrs.target as string)
    const id = slug ? index.get(slug) : undefined
    if (id != null) {
      // Inject the canonical href — existing modifier-click + reader click
      // handlers take it from here.
      decos.push(
        Decoration.node(pos, pos + node.nodeSize, { href: `tela://page/${id}` }),
      )
    } else {
      // Out-of-scope share links render as plain text (no leak); everywhere
      // else an unresolved name is a broken link.
      decos.push(
        Decoration.node(pos, pos + node.nodeSize, {
          class:
            mode === 'share'
              ? 'tela-wikilink--share-out-of-scope'
              : 'tela-wikilink--broken',
        }),
      )
    }
    return false
  })
  return DecorationSet.create(doc, decos)
}

export const wikilinkResolvePlugin = $prose((ctx) => {
  return new Plugin<ResolveState>({
    state: {
      init: (_, { doc }) => ({
        decos: buildResolveDecorations(
          doc,
          ctx.get(wikilinkResolveCtx.key),
          ctx.get(wikilinkModeCtx.key),
        ),
      }),
      apply: (tr, old) => {
        const changed = tr.getMeta(WIKILINK_RESOLVE_META) === true
        if (!tr.docChanged && !changed) return old
        return {
          decos: buildResolveDecorations(
            tr.doc,
            ctx.get(wikilinkResolveCtx.key),
            ctx.get(wikilinkModeCtx.key),
          ),
        }
      },
    },
    props: {
      decorations(state) {
        return this.getState(state)?.decos
      },
    },
  })
})
