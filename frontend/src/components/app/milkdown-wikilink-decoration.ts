import { $ctx, $prose } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'

// Alive-page-ids slice. `null` = the React side hasn't pushed a snapshot yet
// (treat every wikilink as alive so the editor doesn't briefly redline
// everything on first paint). A loaded `Set` is the source of truth: any
// `tela://page/{id}` whose id isn't in the set renders with the broken
// modifier.
export const wikilinkAliveIdsCtx = $ctx<Set<number> | null, 'wikilinkAliveIds'>(
  null,
  'wikilinkAliveIds',
)

// M15.1 — decoration mode. 'edit' (default) is the standard editor surface:
// in-scope wikilinks get the alive style, out-of-scope get tela-wikilink--broken.
// 'share' is the public-share reader surface: in-scope wikilinks still get the
// alive style, but out-of-scope wikilinks render as plain text via the
// tela-wikilink--share-out-of-scope class so we don't leak that a page exists
// outside the share's tree.
export type WikilinkDecorationMode = 'edit' | 'share'
export const wikilinkModeCtx = $ctx<WikilinkDecorationMode, 'wikilinkMode'>(
  'edit',
  'wikilinkMode',
)

// Transactions dispatched by the React side after swapping the slice carry
// this meta so the plugin's `apply` knows to rebuild even without doc changes.
export const WIKILINK_ALIVE_IDS_META = 'tela-wikilink-alive-ids'

interface WikilinkPluginState {
  decos: DecorationSet
}

// Decorates every link mark whose href starts with `tela://page/`. Adds
// `tela-wikilink`; if the target id isn't in the alive set, also adds
// `tela-wikilink--broken` (edit mode) or replaces with
// `tela-wikilink--share-out-of-scope` (share mode). Rebuilds on every doc
// change OR when the React side dispatches the meta-flag after pushing a
// new alive-ids snapshot.
export const wikilinkDecorationPlugin = $prose((ctx) => {
  return new Plugin<WikilinkPluginState>({
    state: {
      init: (_, { doc }) => {
        const aliveIds = ctx.get(wikilinkAliveIdsCtx.key)
        const mode = ctx.get(wikilinkModeCtx.key)
        return { decos: buildWikilinkDecorations(doc, aliveIds, mode) }
      },
      apply: (tr, old) => {
        const aliveChanged = tr.getMeta(WIKILINK_ALIVE_IDS_META) === true
        if (!tr.docChanged && !aliveChanged) return old
        const aliveIds = ctx.get(wikilinkAliveIdsCtx.key)
        const mode = ctx.get(wikilinkModeCtx.key)
        return { decos: buildWikilinkDecorations(tr.doc, aliveIds, mode) }
      },
    },
    props: {
      decorations(state) {
        return this.getState(state)?.decos
      },
    },
  })
})

// Returns the numeric page id, or null for a non-numeric tail (treated as
// broken at the call site). `parseWikiLinks` server-side only emits numeric
// ids, but a hand-typed `tela://page/abc` could still land in the doc.
function parseWikilinkPageId(href: string): number | null {
  const prefix = 'tela://page/'
  if (!href.startsWith(prefix)) return null
  const tail = href.slice(prefix.length)
  if (!/^\d+$/.test(tail)) return null
  return Number(tail)
}

function buildWikilinkDecorations(
  doc: ProseNode,
  aliveIds: Set<number> | null,
  mode: WikilinkDecorationMode,
): DecorationSet {
  const decos: Decoration[] = []
  doc.descendants((node, pos) => {
    if (!node.isInline) return
    const link = node.marks.find((m) => m.type.name === 'link')
    if (!link) return
    const href = link.attrs.href
    if (typeof href !== 'string') return
    // Person mentions: `tela://user/{id}` → a mention chip. No alive-check (the
    // user directory isn't loaded here); always styled as a mention.
    if (href.startsWith('tela://user/')) {
      decos.push(
        Decoration.inline(pos, pos + node.nodeSize, { class: 'tela-mention' }),
      )
      return
    }
    if (!href.startsWith('tela://page/')) return
    let cls = 'tela-wikilink'
    if (aliveIds != null) {
      const id = parseWikilinkPageId(href)
      const inScope = id != null && aliveIds.has(id)
      if (!inScope) {
        cls =
          mode === 'share'
            ? 'tela-wikilink tela-wikilink--share-out-of-scope'
            : 'tela-wikilink tela-wikilink--broken'
      }
    }
    decos.push(Decoration.inline(pos, pos + node.nodeSize, { class: cls }))
  })
  return DecorationSet.create(doc, decos)
}
