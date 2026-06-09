import { $ctx, $nodeSchema, $prose, $remark } from '@milkdown/kit/utils'
import { Plugin } from '@milkdown/kit/prose/state'
import type { Ctx } from '@milkdown/ctx'
import { editorViewCtx } from '@milkdown/kit/core'
import { TextSelection } from '@milkdown/kit/prose/state'
import {
  excalidrawRemark,
  type MdastNode,
} from '../../lib/markdown/transforms/excalidraw'

// Excalidraw fence parsing lives in lib/markdown/transforms/excalidraw.ts
// (Milkdown-free, shared with the view renderer, which renders the server PNG).
// This file keeps the editor-only atom schema + edit-sheet wiring.

// M13.3a — Excalidraw view-mode renderer.
// M13.3b — Edit-Sheet click handler + slash-menu insert helper (this file
// stays plugin-side; the Sheet itself ships in excalidraw-edit-sheet.tsx as
// a lazy chunk and is wired by PageView through `excalidrawOpenCtx`).
//
// Recognizes ```excalidraw\n{json}\n``` markdown fences and materializes them
// as ProseMirror atom nodes that render `<img src=/api/diagrams/{page_id}/{
// scene_hash}.png>` against the M13.2 backend (#111). Read-only view path:
// ZERO Excalidraw runtime, ZERO new npm deps. The Edit Sheet ships in M13.3b
// (#113) as a separate lazy chunk gated by `excalidrawOpenCtx`.
//
// Three pieces wired together:
// 1. `excalidrawRemarkPlugin` — mdast transformer. Matches `code` nodes whose
//    info string is exactly `excalidraw`; parses the body as JSON; extracts
//    `scene_hash` (validated `^[a-f0-9]{8,64}$` to match the backend) and
//    optional `alt_text`; rewrites the node `type` to `excalidraw` carrying
//    the parsed attrs and the raw JSON for round-trip. On parse / hash
//    validation failure the node is left untouched (falls through to a plain
//    code block — current behaviour for an unrecognized info string).
// 2. `excalidrawSchema` — `$nodeSchema('excalidraw', ...)`. ProseMirror atom
//    node (researcher #98 verdict): non-editable inline at the doc level so
//    Yjs sees the whole diagram as one node — every drawing tick inside the
//    Edit Sheet stays out of the live-collab CRDT update stream. `toDOM`
//    renders `<div class="tela-excalidraw"><img src="/api/diagrams/${pageId}/
//    ${sceneHash}.png"></div>`; the browser handles 404 by surfacing the alt
//    text natively (no extra JS). `toMarkdown` re-emits the fence with the
//    original sceneJSON preserved.
// 3. `pageIdCtx` — `$ctx<number>` carrying the active page id so `toDOM` can
//    construct the PNG URL. Wired identically to `wikilinkModeCtx` and
//    `commentThreadsCtx` (passed via React prop → useEffect → ctx.set).


export const pageIdCtx = $ctx<number, 'excalidrawPageId'>(0, 'excalidrawPageId')

// M13.3b — open-Edit-Sheet handler passed in by the host (PageView). Null in
// share-mode / viewer-mode / unmounted state: any click on a diagram is a
// no-op (the atom never shows an edit affordance in those modes because the
// chrome key off `[contenteditable=true]` via `view.editable`).
//
// `pos` is the captured ProseMirror position of the atom node at click /
// insert time. The host's `onSave` callback uses it to dispatch a
// `setNodeMarkup` tx with the freshly-saved scene attrs. Since the Sheet is
// modal-with-overlay, the doc shape can't drift between click and save
// (concurrent collab edits on an atom node are forbidden — atoms are one
// edit unit).
export interface ExcalidrawOpenRequest {
  sceneHash: string
  altText: string
  sceneJSON: string
  onSave: (next: {
    sceneHash: string
    altText: string
    sceneJSON: string
  }) => void
}
export type ExcalidrawOpenHandler = (req: ExcalidrawOpenRequest) => void

export const excalidrawOpenCtx = $ctx<ExcalidrawOpenHandler | null, 'excalidrawOpen'>(
  null,
  'excalidrawOpen',
)

export const excalidrawRemarkPlugin = $remark('telaExcalidraw', () => excalidrawRemark)

// Mirror PM's NodeSpec.toDOM `Node` arg loosely to dodge the cross-package
// Node-type mismatch between `@milkdown/prose/model` and `prosemirror-model`
// (same runtime class, distinct TS types under bundler resolution).
interface ExcalidrawSchemaNode {
  attrs: { sceneHash: string; altText: string; sceneJSON: string }
}

// Minimal valid scene JSON used as the `sceneJSON` for an atom inserted via
// slash menu before the Edit Sheet has been used (M13.3b). Keeps round-trip
// consistent: an "empty" diagram still parses + re-serializes cleanly.
const EMPTY_SCENE_JSON = '{"elements":[],"appState":{},"scene_hash":""}'

export const excalidrawSchema = $nodeSchema('excalidraw', (ctx) => ({
  group: 'block',
  atom: true,
  defining: true,
  draggable: true,
  selectable: true,
  isolating: true,
  marks: '',
  attrs: {
    sceneHash: { default: '' },
    altText: { default: '' },
    sceneJSON: { default: '' },
  },
  parseDOM: [
    {
      tag: 'div.tela-excalidraw[data-scene-hash]',
      getAttrs: (dom) => {
        const el = dom as HTMLElement
        return {
          sceneHash: el.getAttribute('data-scene-hash') ?? '',
          altText: el.getAttribute('data-alt-text') ?? '',
          sceneJSON: el.getAttribute('data-scene-json') ?? '',
        }
      },
    },
  ],
  toDOM: (node) => {
    const { sceneHash, altText, sceneJSON } = (node as unknown as ExcalidrawSchemaNode).attrs
    const pageId = ctx.get(pageIdCtx.key)
    // M13.3b — the hover-edit affordance. CSS controls visibility (hidden
    // unless wrapper is hovered AND the editor is editable, gated by the
    // `[contenteditable='true']` ancestor selector in editor.css). The
    // button click bubbles up to the wrapper div where the
    // `excalidrawClickPlugin` catches it and dispatches the open callback.
    const editBtn: ['button', Record<string, string>, string] = [
      'button',
      {
        type: 'button',
        class: 'tela-excalidraw-edit-btn',
        contenteditable: 'false',
        'aria-label': 'Edit diagram',
      },
      'Edit',
    ]
    if (!sceneHash) {
      // Newly-inserted atom (slash menu) with no PNG yet. Placeholder chrome
      // hints the user to open the Edit Sheet.
      return [
        'div',
        {
          class: 'tela-excalidraw tela-excalidraw--empty',
          'data-scene-hash': '',
          'data-alt-text': altText,
          'data-scene-json': sceneJSON,
        },
        ['span', { class: 'tela-excalidraw-empty-label' }, '[Empty diagram — click to draw]'],
        editBtn,
      ]
    }
    return [
      'div',
      {
        class: 'tela-excalidraw',
        'data-scene-hash': sceneHash,
        'data-alt-text': altText,
        'data-scene-json': sceneJSON,
      },
      [
        'img',
        {
          src: `/api/diagrams/${pageId}/${sceneHash}.png`,
          alt: altText || 'Excalidraw diagram',
          loading: 'lazy',
        },
      ],
      editBtn,
    ]
  },
  parseMarkdown: {
    match: ({ type }) => type === 'excalidraw',
    runner: (state, node, type) => {
      const n = node as MdastNode
      state.addNode(type, {
        sceneHash: typeof n.sceneHash === 'string' ? n.sceneHash : '',
        altText: typeof n.altText === 'string' ? n.altText : '',
        sceneJSON:
          typeof n.sceneJSON === 'string' && n.sceneJSON.length > 0
            ? n.sceneJSON
            : EMPTY_SCENE_JSON,
      })
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'excalidraw',
    runner: (state, node) => {
      const sceneJSON =
        typeof node.attrs.sceneJSON === 'string' && node.attrs.sceneJSON.length > 0
          ? (node.attrs.sceneJSON as string)
          : EMPTY_SCENE_JSON
      // mdast `code` node with lang=excalidraw round-trips through
      // remark-stringify as a ```excalidraw fence with the value as body —
      // identical to the source markdown. We don't go through `state.write`
      // / `state.text` directly because that bypasses remark's fence-marker
      // detection (would emit indented blocks instead of fences when the
      // body contains backticks).
      state.addNode('code', undefined, sceneJSON, { lang: 'excalidraw' })
    },
  },
}))

// M13.3b — click handler. Intercepts clicks on any `.tela-excalidraw` wrapper
// in editable mode and invokes the host's Edit-Sheet open callback with the
// current atom attrs + an onSave that dispatches `setNodeMarkup` at the node
// position. In read-only mode (`view.editable === false`) the click is a
// no-op (and the hover Edit button is hidden via CSS). In share-mode the
// host registers a null open handler, so the plugin is mounted but never
// fires anything.
export const excalidrawClickPlugin = $prose((ctx) => {
  return new Plugin({
    props: {
      handleDOMEvents: {
        click: (view, event) => {
          if (!view.editable) return false
          const targetEl =
            (event.target instanceof Element &&
              event.target.closest('.tela-excalidraw')) ||
            null
          if (!targetEl) return false
          const openCb = ctx.get(excalidrawOpenCtx.key)
          if (!openCb) return false

          const pos = view.posAtDOM(targetEl, 0)
          if (pos < 0) return false
          const node = view.state.doc.nodeAt(pos)
          if (!node || node.type.name !== 'excalidraw') return false

          const sceneHash = typeof node.attrs.sceneHash === 'string' ? node.attrs.sceneHash : ''
          const altText = typeof node.attrs.altText === 'string' ? node.attrs.altText : ''
          const sceneJSON = typeof node.attrs.sceneJSON === 'string' ? node.attrs.sceneJSON : ''

          openCb({
            sceneHash,
            altText,
            sceneJSON,
            onSave: (next) => {
              const tr = view.state.tr.setNodeMarkup(pos, undefined, {
                sceneHash: next.sceneHash,
                altText: next.altText,
                sceneJSON: next.sceneJSON,
              })
              view.dispatch(tr)
            },
          })
          event.preventDefault()
          return true
        },
      },
    },
  })
})

// M13.3b — slash-menu insert helper. Constructs an empty `excalidraw` atom
// node, replaces the current selection with it, and immediately opens the
// Edit Sheet at the inserted position so the user lands in the canvas. The
// approach mirrors `insertCollapsible` from M13.1 (walk the post-replacement
// doc to find the newly-inserted node by attribute — robust to PM's
// positioning quirks for atoms at various contexts).
export function insertExcalidraw(ctx: Ctx): void {
  const view = ctx.get(editorViewCtx)
  const { state } = view
  const excalidrawType = state.schema.nodes.excalidraw
  if (!excalidrawType) return
  const openCb = ctx.get(excalidrawOpenCtx.key)

  const atom = excalidrawType.create({ sceneHash: '', altText: '', sceneJSON: '' })
  const tr = state.tr.replaceSelectionWith(atom)
  // Find the inserted atom: it's the LAST excalidraw node in the post-tr
  // doc whose sceneHash is empty (the user can't realistically have other
  // empty atoms in the doc — they're inserted only via this helper and
  // immediately get a hash on first save). Even if they did, the worst case
  // is opening the Sheet on a different empty atom — harmless.
  let insertedPos = -1
  tr.doc.descendants((node, pos) => {
    if (node.type === excalidrawType && node.attrs.sceneHash === '') {
      insertedPos = pos
    }
    return true
  })
  if (insertedPos !== -1) {
    // Park the selection just after the atom so a follow-up keystroke lands
    // on a sensible position (or NodeSelection on the atom itself if PM
    // prefers — `Selection.near` resolves to a valid neighbor).
    tr.setSelection(TextSelection.near(tr.doc.resolve(insertedPos + atom.nodeSize)))
  }
  view.dispatch(tr.scrollIntoView())
  if (insertedPos !== -1 && openCb) {
    // Capture the position locally; the onSave closure dispatches at the
    // post-insertion position. The Sheet opens on top of the just-inserted
    // empty placeholder; saving promotes it to a populated diagram.
    openCb({
      sceneHash: '',
      altText: '',
      sceneJSON: '',
      onSave: (next) => {
        const v = ctx.get(editorViewCtx)
        const node = v.state.doc.nodeAt(insertedPos)
        if (!node || node.type !== excalidrawType) return
        const tr2 = v.state.tr.setNodeMarkup(insertedPos, undefined, {
          sceneHash: next.sceneHash,
          altText: next.altText,
          sceneJSON: next.sceneJSON,
        })
        v.dispatch(tr2)
      },
    })
  }
}
