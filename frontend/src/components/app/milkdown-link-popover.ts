import { $prose } from '@milkdown/kit/utils'
import { Plugin, TextSelection } from '@milkdown/kit/prose/state'
import { commandsCtx } from '@milkdown/kit/core'
import {
  toggleLinkCommand,
  updateLinkCommand,
} from '@milkdown/kit/preset/commonmark'
import type { EditorView } from '@milkdown/kit/prose/view'
import { positionFloating, setShow } from './milkdown-floating'

// Hover a link in the editor → a small popover to open / copy / edit / remove it,
// without having to select the link text first. Pure DOM (no React adapter): a
// hover popover is pointer-driven, not PM-transaction-driven, so it manages its
// own element + show/hide bridge and rides the shared positionFloating helper.
// Edit/Remove reuse the same commonmark link commands the bubble toolbar uses.

const HIDE_DELAY = 160

// Resolve the link mark's doc range from its <a> element. A link renders as one
// contiguous <a>, so its text length maps to the span it covers — good enough to
// re-select for update/remove (exotic links spanning inline atoms are rare).
function linkRange(
  view: EditorView,
  a: HTMLAnchorElement,
): { from: number; to: number } | null {
  try {
    const from = view.posAtDOM(a, 0)
    const to = from + (a.textContent?.length ?? 0)
    return to > from ? { from, to } : null
  } catch {
    return null
  }
}

export const linkPopoverPlugin = $prose((ctx) => {
  let el: HTMLElement | null = null
  let urlEl: HTMLAnchorElement | null = null
  let input: HTMLInputElement | null = null
  let current: HTMLAnchorElement | null = null
  let view: EditorView | null = null
  let hideTimer: number | null = null

  const cancelHide = () => {
    if (hideTimer != null) {
      clearTimeout(hideTimer)
      hideTimer = null
    }
  }
  const scheduleHide = () => {
    cancelHide()
    hideTimer = window.setTimeout(hide, HIDE_DELAY)
  }
  function hide() {
    cancelHide()
    if (el) {
      setShow(el, false)
      el.dataset.mode = 'view'
    }
    current = null
  }

  // Re-select the hovered link in the doc, then run a commonmark link command.
  function withLinkSelected(run: () => void) {
    if (!current || !view) return
    const range = linkRange(view, current)
    if (!range) return
    view.dispatch(
      view.state.tr.setSelection(
        TextSelection.create(view.state.doc, range.from, range.to),
      ),
    )
    view.focus()
    run()
    hide()
  }

  function button(label: string, onClick: () => void) {
    const b = document.createElement('button')
    b.type = 'button'
    b.className = 'tela-link-popover-btn'
    b.textContent = label
    b.addEventListener('mousedown', (e) => e.preventDefault()) // keep editor selection
    b.addEventListener('click', (e) => {
      e.preventDefault()
      onClick()
    })
    return b
  }

  function build() {
    if (el) return
    el = document.createElement('div')
    el.className = 'tela-link-popover'
    el.dataset.show = 'false'
    el.dataset.mode = 'view'

    urlEl = document.createElement('a')
    urlEl.className = 'tela-link-popover-url'
    urlEl.target = '_blank'
    urlEl.rel = 'noreferrer noopener'
    const viewRow = document.createElement('div')
    viewRow.className = 'tela-link-popover-row tela-link-popover-view'
    viewRow.append(
      urlEl,
      button('Copy', () => {
        const href = current?.getAttribute('href')
        if (href) void navigator.clipboard?.writeText(href)
        hide()
      }),
      button('Edit', enterEdit),
      button('Remove', () =>
        withLinkSelected(() =>
          ctx.get(commandsCtx).call(toggleLinkCommand.key),
        ),
      ),
    )

    input = document.createElement('input')
    input.className = 'tela-link-popover-input'
    input.type = 'text'
    input.placeholder = 'https://…'
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault()
        saveEdit()
      } else if (e.key === 'Escape') {
        e.preventDefault()
        if (el) el.dataset.mode = 'view'
      }
    })
    const editRow = document.createElement('div')
    editRow.className = 'tela-link-popover-row tela-link-popover-edit'
    editRow.append(input, button('Save', saveEdit))

    el.append(viewRow, editRow)
    el.addEventListener('mouseenter', cancelHide)
    el.addEventListener('mouseleave', scheduleHide)
    document.body.appendChild(el)
  }

  function enterEdit() {
    if (!el || !input || !current) return
    input.value = current.getAttribute('href') ?? ''
    el.dataset.mode = 'edit'
    input.focus()
    input.select()
  }
  function saveEdit() {
    const href = input?.value.trim()
    if (!href) return
    withLinkSelected(() =>
      ctx.get(commandsCtx).call(updateLinkCommand.key, { href }),
    )
  }

  function show(a: HTMLAnchorElement) {
    build()
    if (!el || !urlEl) return
    if (current === a) {
      cancelHide()
      return
    }
    current = a
    el.dataset.mode = 'view'
    const href = a.getAttribute('href') ?? ''
    urlEl.textContent = href
    urlEl.href = a.href
    cancelHide()
    setShow(el, true)
    const rect = a.getBoundingClientRect()
    positionFloating(
      el,
      { top: rect.top, bottom: rect.bottom, left: rect.left },
      { place: 'above', align: 'start' },
    )
    // Hide when the pointer leaves this link (cancelled if it enters the popover).
    a.addEventListener('mouseleave', scheduleHide, { once: true })
  }

  return new Plugin({
    view(editorView) {
      view = editorView
      build()
      return {
        destroy() {
          hide()
          el?.remove()
          el = null
          view = null
        },
      }
    },
    props: {
      handleDOMEvents: {
        mouseover(editorView, event) {
          if (!editorView.editable) return false
          const a = (event.target as HTMLElement | null)?.closest('a') as
            | HTMLAnchorElement
            | null
          if (!a || !editorView.dom.contains(a)) return false
          // Skip wikilinks / internal atoms — they have their own affordances and
          // aren't plain editable links.
          const href = a.getAttribute('href') ?? ''
          if (href.startsWith('tela:') || a.closest('.tela-wikilink'))
            return false
          show(a)
          return false
        },
      },
    },
  })
})
