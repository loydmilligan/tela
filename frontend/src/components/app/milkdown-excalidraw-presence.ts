import { $ctx, $prose } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'
import { Decoration, DecorationSet } from '@milkdown/kit/prose/view'
import type { Node as ProseNode } from '@milkdown/kit/prose/model'

// Editor-only live presence for Excalidraw diagrams. When another collaborator
// has a diagram's edit sheet open, its in-page block gets an "editing" badge so
// you know it's being worked on without opening it.
//
// Source of truth is page awareness (each editor publishes `editingDiagramId`
// while its sheet is open; see milkdown-editor.tsx). The React side reads that
// via useDiagramEditors and pushes a Map<diagramId, usernames[]> into the ctx
// slice below, then dispatches a transaction carrying EXCALIDRAW_PRESENCE_META
// so this plugin re-decorates even without a doc change — the exact pattern
// wikilink-decoration.ts uses for its alive-ids snapshot.
//
// Rendering is a NODE decoration: it toggles a class + a `data-editing-label`
// attribute on the existing `.tela-excalidraw` wrapper, and CSS draws the pill
// via ::after. No widget DOM injection, no React in the PM tree.
//
// Editor-only by design: the read-mode view renderer is deliberately collab-
// free (no awareness connection), so presence simply doesn't apply there.

export const excalidrawPresenceCtx = $ctx<
  Map<string, string[]>,
  'excalidrawPresence'
>(new Map(), 'excalidrawPresence')

export const EXCALIDRAW_PRESENCE_META = 'tela-excalidraw-presence'

interface PresencePluginState {
  decos: DecorationSet
}

export const excalidrawPresencePlugin = $prose((ctx) => {
  return new Plugin<PresencePluginState>({
    state: {
      init: (_, { doc }) => ({
        decos: buildPresenceDecorations(doc, ctx.get(excalidrawPresenceCtx.key)),
      }),
      apply: (tr, old) => {
        const presenceChanged = tr.getMeta(EXCALIDRAW_PRESENCE_META) === true
        if (!tr.docChanged && !presenceChanged) return old
        return {
          decos: buildPresenceDecorations(
            tr.doc,
            ctx.get(excalidrawPresenceCtx.key),
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

function buildPresenceDecorations(
  doc: ProseNode,
  editors: Map<string, string[]>,
): DecorationSet {
  if (editors.size === 0) return DecorationSet.empty
  const decos: Decoration[] = []
  doc.descendants((node, pos) => {
    if (node.type.name !== 'excalidraw') return
    const diagramId = node.attrs.diagramId
    if (typeof diagramId !== 'string' || diagramId === '') return
    const names = editors.get(diagramId)
    if (!names || names.length === 0) return
    decos.push(
      Decoration.node(pos, pos + node.nodeSize, {
        class: 'tela-excalidraw--being-edited',
        'data-editing-label': editingLabel(names),
      }),
    )
  })
  return DecorationSet.create(doc, decos)
}

// "Alice editing" for one, "Alice +1 editing" / "3 editing" for more.
function editingLabel(names: string[]): string {
  if (names.length === 1) return `${names[0]} editing`
  if (names.length === 2) return `${names[0]} +1 editing`
  return `${names.length} editing`
}
