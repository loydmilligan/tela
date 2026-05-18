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
import { slashPlugin, SlashView } from './milkdown-slash'
import { wikilinkPlugin, WikilinkView } from './milkdown-wikilink'
import { wikilinkDecorationPlugin } from './milkdown-wikilink-decoration'

export interface MilkdownEditorProps {
  defaultValue: string
  onChange: (markdown: string) => void
  onBlur?: () => void
  autoFocus?: boolean
  ariaLabel?: string
  className?: string
}

function MilkdownEditorInner({
  defaultValue,
  onChange,
  onBlur,
  autoFocus,
  ariaLabel,
  className,
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
      .use(wikilinkDecorationPlugin),
  )

  useEffect(() => {
    if (loading || !autoFocus) return
    const editor = get()
    editor?.action((ctx) => {
      ctx.get(editorViewCtx).focus()
    })
  }, [loading, autoFocus, get])

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
