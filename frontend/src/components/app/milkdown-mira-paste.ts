import type { Ctx } from '@milkdown/ctx'
import { Plugin } from '@milkdown/kit/prose/state'
import { $ctx } from '@milkdown/kit/utils'

// M18.B.2 — paste-hook that detects a single mira (mira.cagdas.io) URL pasted
// into the editor and hands the event off to React for an inline "import /
// keep as link / cancel" popover. Pure ProseMirror plugin in this file — the
// popover component lives in the companion `milkdown-mira-paste-popover.tsx`.
//
// Wiring shape (mirrors `excalidrawOpenCtx` from milkdown-excalidraw.ts):
// 1. `miraPasteRequestCtx` — the editor host (MilkdownEditor) injects a
//    request handler that pops a React popover. `null` short-circuits the
//    plugin (used in viewer / share / no-pageId modes).
// 2. `createMiraPastePlugin(ctx)` — `handlePaste` that matches a tight
//    `https?://mira.cagdas.io/p/<slug>` clipboard string, suppresses the
//    default paste, captures the caret's screen coords for popover
//    anchoring, and invokes the request handler with three callbacks for the
//    three popover actions:
//      • `insertWikilink(pageId, title)` — used after a successful import.
//        Inserts `[Title](tela://page/{id})` (the canonical wikilink format)
//        at the current selection. The wikilink-decoration plugin paints it
//        alive on the next decoration tick.
//      • `insertPlainLink()` — used for "Keep as link" and as an error
//        fallback. Inserts the URL as a normal markdown link.
//      • The popover dismiss / cancel cases insert nothing.
//
// Ordering: the host inserts this PM Plugin at the FRONT of `prosePluginsCtx`
// so its `handlePaste` runs before the collab branch's ySync/yCursor/yUndo
// plugins in collab mode. ProseMirror invokes `handlePaste` handlers in
// plugin order; the first to return `true` wins, so being first guarantees
// we beat any built-in clipboard-text parser if it ever grows one.
//
// View-mode / no-page-id guards live host-side: the host doesn't register a
// handler in those cases, so this plugin's `handlePaste` returns false early
// and lets the default URL-as-link paste behaviour fall through. We also
// re-check `view.editable` defensively — read-only mid-session (collab
// disconnect → reconnecting banner) shouldn't surface an action popover.

export interface MiraPasteRequest {
  // The pasted URL, trimmed.
  url: string
  // Screen-space coordinates of the caret at paste time. The popover anchors
  // its top-left at `{left, bottom}` so it appears just below the caret, and
  // the host re-measures one rAF later to flip / clamp if it would overflow.
  anchor: { left: number; top: number; bottom: number }
  // Replace the paste position with a wikilink mark + trailing unmarked space.
  // Reads `view.state` fresh at call time so the selection mapping survives
  // any concurrent collab edits that may have landed between paste and
  // action-button click.
  insertWikilink: (pageId: number, title: string) => void
  // Insert the URL as a normal markdown auto-link. Used by "Keep as link" and
  // as the error fallback when an import request fails.
  insertPlainLink: () => void
}

export type MiraPasteRequestHandler = (req: MiraPasteRequest) => void

export const miraPasteRequestCtx = $ctx<
  MiraPasteRequestHandler | null,
  'miraPasteRequest'
>(null, 'miraPasteRequest')

// Tight match: a single mira page URL with no surrounding text and no
// whitespace inside the URL. We trim the clipboard text first so a trailing
// newline (common when copying from a browser's URL bar on some platforms)
// doesn't disqualify the match.
const MIRA_URL_RE = /^https?:\/\/mira\.cagdas\.io\/p\/\S+$/

export function createMiraPastePlugin(ctx: Ctx): Plugin {
  return new Plugin({
    props: {
      handlePaste: (view, event) => {
        const handler = ctx.get(miraPasteRequestCtx.key)
        if (!handler) return false
        if (!view.editable) return false
        const clip = event.clipboardData
        if (!clip) return false
        const text = clip.getData('text/plain').trim()
        if (!text || !MIRA_URL_RE.test(text)) return false

        const { from } = view.state.selection
        const coords = view.coordsAtPos(from)

        handler({
          url: text,
          anchor: {
            left: coords.left,
            top: coords.top,
            bottom: coords.bottom,
          },
          insertWikilink: (pageId, title) => {
            const state = view.state
            const linkType = state.schema.marks.link
            if (!linkType) return
            const mark = linkType.create({
              href: `tela://page/${pageId}`,
              title: null,
            })
            const linkText = state.schema.text(title || '(untitled)', [mark])
            const spaceText = state.schema.text(' ', [])
            const { from, to } = state.selection
            view.dispatch(
              state.tr
                .replaceWith(from, to, [linkText, spaceText])
                .setStoredMarks([])
                .scrollIntoView(),
            )
            view.focus()
          },
          insertPlainLink: () => {
            const state = view.state
            const linkType = state.schema.marks.link
            if (!linkType) return
            const mark = linkType.create({ href: text, title: null })
            const linkText = state.schema.text(text, [mark])
            const spaceText = state.schema.text(' ', [])
            const { from, to } = state.selection
            view.dispatch(
              state.tr
                .replaceWith(from, to, [linkText, spaceText])
                .setStoredMarks([])
                .scrollIntoView(),
            )
            view.focus()
          },
        })
        return true
      },
    },
  })
}
