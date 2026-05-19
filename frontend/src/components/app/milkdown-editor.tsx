import { useEffect, useRef, useState } from 'react'
import {
  Editor,
  defaultValueCtx,
  editorViewCtx,
  editorViewOptionsCtx,
  parserCtx,
  prosePluginsCtx,
  rootCtx,
} from '@milkdown/kit/core'
import { commonmark, imageAttr } from '@milkdown/kit/preset/commonmark'
import { gfm } from '@milkdown/kit/preset/gfm'
import { history } from '@milkdown/kit/plugin/history'
import { clipboard } from '@milkdown/kit/plugin/clipboard'
import { listener, listenerCtx } from '@milkdown/kit/plugin/listener'
import { Milkdown, MilkdownProvider, useEditor } from '@milkdown/react'
import {
  ProsemirrorAdapterProvider,
  usePluginViewFactory,
} from '@prosemirror-adapter/react'
import { prism } from '@milkdown/plugin-prism'
import * as Y from 'yjs'
import { prosemirrorToYXmlFragment, ySyncPlugin, yUndoPlugin } from 'y-prosemirror'
import { cn } from '../../lib/utils'
import { emitOpenNewPage } from '../../lib/newPageEvent'
import { TelaProvider, type TelaProviderStatus } from '../../lib/collab/tela-provider'
import { slashPlugin, SlashView } from './milkdown-slash'
import { wikilinkPlugin, WikilinkView } from './milkdown-wikilink'
import {
  WIKILINK_ALIVE_IDS_META,
  wikilinkAliveIdsCtx,
  wikilinkDecorationPlugin,
} from './milkdown-wikilink-decoration'

export interface MilkdownEditorProps {
  defaultValue: string
  onChange: (markdown: string) => void
  onBlur?: () => void
  autoFocus?: boolean
  ariaLabel?: string
  className?: string
  // Live snapshot of page ids that still exist. `null` means "don't know yet"
  // (e.g. the `/api/pages/all` query hasn't returned) — the decoration plugin
  // treats every wikilink as alive in that state to avoid a redline flash on
  // first paint. M5.2d.
  aliveWikilinkIds?: Set<number> | null
  // M7.2 LiveCollab. When set, the editor opens a Yjs WebSocket session
  // against /ws/pages/{collabPageId} via our custom 5-tag wire protocol
  // (NOT y-websocket). y-prosemirror's sync + undo plugins are wired in;
  // ws drop flips the editor to read-only with a "Reconnecting…" banner.
  //
  // `collabPageId` is intentionally separate from `defaultValue`: the
  // PageView keys the entire PageEditor subtree on page id, so a page
  // switch unmounts/remounts this component — Y.Doc + provider are owned
  // by this instance and torn down on unmount. Passing the id explicitly
  // (rather than parsing from URL) keeps the component testable.
  //
  // When null/undefined, the editor renders in legacy non-collab mode
  // (used for viewer fallback in PageView).
  collabPageId?: number | null
  // When true, the editor is permanently read-only — used for viewer-role
  // fallback. Takes precedence over the collab connection state.
  readOnly?: boolean
}

// Reconnecting banner copy.
const RECONNECTING_LABEL = 'Reconnecting…'

function MilkdownEditorInner({
  defaultValue,
  onChange,
  onBlur,
  autoFocus,
  ariaLabel,
  className,
  aliveWikilinkIds,
  collabPageId,
  readOnly,
}: MilkdownEditorProps) {
  const pluginViewFactory = usePluginViewFactory()

  // Keep callbacks fresh without recreating the editor on every render.
  const callbacks = useRef({ onChange, onBlur })
  callbacks.current = { onChange, onBlur }

  // M7.2 — lazy-init Y.Doc + TelaProvider in a stable ref so the editor
  // factory captures them once. Y.Doc lifecycle pitfall: a re-render with
  // a non-stable doc would trash collab state on every parent update.
  const collabRef = useRef<{ doc: Y.Doc; provider: TelaProvider } | null>(
    null,
  )
  if (collabPageId != null && collabRef.current == null) {
    const doc = new Y.Doc()
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${proto}//${window.location.host}/ws/pages/${collabPageId}`
    collabRef.current = { doc, provider: new TelaProvider(url, doc) }
  }

  const [collabStatus, setCollabStatus] = useState<TelaProviderStatus>(
    () => collabRef.current?.provider.getStatus() ?? 'connected',
  )
  useEffect(() => {
    const collab = collabRef.current
    if (!collab) return
    // Re-read status on attach so we don't miss a transition that happened
    // between useState init and effect mount (race-prone for fast localhost
    // connections where ws.onopen + sync-init can land before paint).
    setCollabStatus(collab.provider.getStatus())
    return collab.provider.onStatus(setCollabStatus)
  }, [])

  // Editable predicate. PM evaluates this on every updateState. We use a
  // ref-backed read so the predicate's identity is stable but its result
  // tracks live state. A no-op tx is dispatched below on
  // status/readOnly changes to force PM to re-read.
  const editableRef = useRef(true)
  editableRef.current =
    !readOnly && (collabRef.current == null || collabStatus === 'connected')

  const { loading, get } = useEditor((root) =>
    Editor.make()
      .config((ctx) => {
        ctx.set(rootCtx, root)
        ctx.set(defaultValueCtx, defaultValue)
        ctx
          .get(listenerCtx)
          .markdownUpdated((_, md, prev) => {
            if (md !== prev) callbacks.current.onChange(md)
          })
          .blur(() => callbacks.current.onBlur?.())
        ctx.set(slashPlugin.key, {
          view: pluginViewFactory({ component: SlashView }),
        })
        ctx.set(wikilinkPlugin.key, {
          view: pluginViewFactory({ component: WikilinkView }),
        })
        ctx.set(imageAttr.key, () => ({ loading: 'lazy' }))

        // M7.2: wire the editable toggle. Function form is re-evaluated by
        // PM on every updateState; banner-driven readonly + ws-disconnect
        // both flow through editableRef.
        ctx.set(editorViewOptionsCtx, {
          editable: () => editableRef.current,
        })

        // M7.2: bind y-prosemirror's sync + undo plugins when collab is
        // active. ySyncPlugin observes the Y.XmlFragment and pushes the
        // initial PM doc into it iff the fragment is empty — naturally
        // seeding a fresh room from defaultValueCtx (the canonical
        // pages.body markdown) without us doing anything special.
        const collab = collabRef.current
        if (collab) {
          const fragment = collab.doc.getXmlFragment('milkdown-doc')
          ctx.update(prosePluginsCtx, (existing) => [
            ...existing,
            ySyncPlugin(fragment),
            yUndoPlugin(),
          ])
        }
      })
      .use(commonmark)
      .use(gfm)
      .use(history)
      .use(clipboard)
      .use(listener)
      .use(prism)
      .use(slashPlugin)
      .use(wikilinkPlugin)
      .use(wikilinkAliveIdsCtx)
      .use(wikilinkDecorationPlugin),
  )

  useEffect(() => {
    if (loading || !autoFocus) return
    const editor = get()
    editor?.action((ctx) => {
      ctx.get(editorViewCtx).focus()
    })
  }, [loading, autoFocus, get])

  // Push the alive-ids snapshot into the milkdown ctx and dispatch a no-op
  // transaction tagged with the meta flag so the decoration plugin's `apply`
  // reruns and repaints. Without the meta-tx the plugin would only refresh on
  // the next user keystroke.
  useEffect(() => {
    if (loading) return
    const editor = get()
    editor?.action((ctx) => {
      ctx.set(wikilinkAliveIdsCtx.key, aliveWikilinkIds ?? null)
      const view = ctx.get(editorViewCtx)
      view.dispatch(view.state.tr.setMeta(WIKILINK_ALIVE_IDS_META, true))
    })
  }, [loading, get, aliveWikilinkIds])

  // M7.2: force PM to re-read the `editable` predicate when collab status
  // or readOnly flips. PM only re-evaluates editable inside updateState,
  // and a disconnect with no pending edits wouldn't otherwise trigger one.
  // A no-op tx dispatch repaints the contenteditable attribute.
  useEffect(() => {
    if (loading) return
    const editor = get()
    editor?.action((ctx) => {
      const view = ctx.get(editorViewCtx)
      view.dispatch(view.state.tr)
    })
  }, [loading, get, collabStatus, readOnly])

  // M7.2: empty-room seed from canonical markdown body. ySyncPlugin's
  // view() pulls fragment → PM on attach — it does NOT push initial PM
  // content into the fragment. So a fresh page (no server-side Yjs state
  // yet) would render empty after the plugin attaches even though
  // pages.body is non-empty. We re-parse `defaultValue` via Milkdown's
  // parser and push it to the Y.XmlFragment via prosemirrorToYXmlFragment
  // after the first sync-init confirms the room is genuinely fresh.
  //
  // Multi-client race: two clients opening a fresh page simultaneously
  // both seed; Yjs CRDT would merge as duplicated content. Acceptable for
  // v0 — extremely rare in practice (a brand-new page open by two users
  // in the same ~100ms before either has typed). #64's leader election
  // doesn't address this; a deterministic seed lock is out of M7 scope.
  useEffect(() => {
    if (loading) return
    const collab = collabRef.current
    if (!collab) return
    const editor = get()
    if (!editor) return
    if (!defaultValue || defaultValue.trim().length === 0) return

    let cancelled = false
    const unsub = collab.provider.onFirstSync(({ hadServerState }) => {
      if (cancelled || hadServerState) return
      const fragment = collab.doc.getXmlFragment('milkdown-doc')
      if (fragment.length > 0) return
      editor.action((ctx) => {
        const parser = ctx.get(parserCtx)
        const pmNode = parser(defaultValue)
        if (pmNode) {
          prosemirrorToYXmlFragment(pmNode, fragment)
        }
      })
    })
    return () => {
      cancelled = true
      unsub()
    }
  }, [loading, get, defaultValue])

  // Broken-wikilink click → emit a request to open the new-page dialog with
  // the link text pre-filled. We hang the listener on the editor's
  // contenteditable dom so prevention happens before ProseMirror would try to
  // follow the dead `tela://` URL.
  useEffect(() => {
    if (loading) return
    const editor = get()
    if (!editor) return
    const domBox: { el: HTMLElement | null } = { el: null }
    editor.action((ctx) => {
      domBox.el = ctx.get(editorViewCtx).dom as HTMLElement
    })
    const dom = domBox.el
    if (!dom) return
    const onClick = (e: MouseEvent) => {
      const target = e.target as HTMLElement | null
      if (!target) return
      const broken = target.closest('.tela-wikilink--broken')
      if (!broken) return
      e.preventDefault()
      const title = (broken.textContent ?? '').trim()
      emitOpenNewPage({ prefillTitle: title })
    }
    dom.addEventListener('click', onClick)
    return () => dom.removeEventListener('click', onClick)
  }, [loading, get])

  // Teardown: close ws + cancel reconnect + dispose Y.Doc on unmount. Without
  // this, tabbing between pages would leak rooms and reconnect timers.
  useEffect(() => {
    return () => {
      const collab = collabRef.current
      if (!collab) return
      collab.provider.destroy()
      collab.doc.destroy()
      collabRef.current = null
    }
  }, [])

  const showReconnectBanner =
    !readOnly && collabRef.current != null && collabStatus !== 'connected'

  return (
    <div className={cn('tela-milkdown', className)} aria-label={ariaLabel}>
      {showReconnectBanner ? (
        <div
          role="status"
          aria-live="polite"
          className={cn(
            'mb-[var(--space-2)] px-[var(--space-3)] py-[var(--space-2)]',
            'text-[length:var(--text-sm)] text-[var(--text-muted)]',
            'bg-[var(--surface-2)] border border-[var(--border-subtle)]',
            'rounded-[var(--radius-sm)]',
          )}
        >
          {RECONNECTING_LABEL}
        </div>
      ) : null}
      <Milkdown />
    </div>
  )
}

export function MilkdownEditor(props: MilkdownEditorProps) {
  return (
    <MilkdownProvider>
      <ProsemirrorAdapterProvider>
        <MilkdownEditorInner {...props} />
      </ProsemirrorAdapterProvider>
    </MilkdownProvider>
  )
}
