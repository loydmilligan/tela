import { useEffect, useRef } from 'react'
import {
  Editor,
  defaultValueCtx,
  editorViewCtx,
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
import { cn } from '../../lib/utils'
import { emitOpenNewPage } from '../../lib/newPageEvent'
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
}

function MilkdownEditorInner({
  defaultValue,
  onChange,
  onBlur,
  autoFocus,
  ariaLabel,
  className,
  aliveWikilinkIds,
}: MilkdownEditorProps) {
  const pluginViewFactory = usePluginViewFactory()

  // Keep callbacks fresh without recreating the editor on every render.
  const callbacks = useRef({ onChange, onBlur })
  callbacks.current = { onChange, onBlur }

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

  return (
    <div className={cn('tela-milkdown', className)} aria-label={ariaLabel}>
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
