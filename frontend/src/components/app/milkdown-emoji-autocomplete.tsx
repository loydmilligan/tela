import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { slashFactory } from '@milkdown/kit/plugin/slash'
import { usePluginViewContext } from '@prosemirror-adapter/react'
import { useInstance } from '@milkdown/react'
import { editorViewCtx } from '@milkdown/kit/core'
import type { EditorView } from '@milkdown/kit/prose/view'
import { setPos, setShow } from './milkdown-floating'
import {
  ensureEmojiLoaded,
  searchEmoji,
  type EmojiHit,
} from '../../lib/emoji'

// Caret-anchored `:shortcode:` autocomplete. Typing `:roc…` opens a ranked
// picker; selecting inserts the Unicode emoji (canonical markdown stores the
// char, not the shortcode — see lib/emoji.ts). Complements the input rule in
// milkdown-emoji.ts, which auto-converts a fully-typed `:rocket:`.
//
// Positioning is managed manually (view.coordsAtPos + setShow/setPos),
// mirroring milkdown-slash.tsx rather than the wikilink picker. We deliberately
// do NOT use SlashProvider: its internal lodash.debounce wedges under our
// React + Yjs + Vite setup, and the emoji dataset loads ASYNC — so the menu
// only needs to appear on an update AFTER the data resolves, which is exactly
// the update the wedged debounce swallows (the wikilink picker escapes this
// because its page list is already cached on first `[[`). See the SlashView
// comment block for the full rationale.

export const emojiAutocompletePlugin = slashFactory('tela-emoji')

interface EmojiTrigger {
  query: string
  // Offset of the `:` within the parent paragraph; insertion replaces from
  // here to the cursor so the `:query` text is consumed.
  openOffset: number
}

// Minimum query length before the menu opens — keeps an isolated `:` (or a
// one-letter `:a`) from flickering the picker on ordinary prose.
const MIN_QUERY = 2
const QUERY_RE = /^[a-z0-9_+-]+$/

function readEmojiState(view: EditorView): EmojiTrigger | null {
  const { selection } = view.state
  const { empty, $from } = selection
  if (!empty) return null
  if ($from.parent.type.name !== 'paragraph') return null
  const text = $from.parent.textBetween(0, $from.parentOffset, undefined, '￼')
  const colonIdx = text.lastIndexOf(':')
  if (colonIdx < 0) return null
  // The `:` must start the block or follow whitespace — so `12:30`, `Note:`,
  // and `foo:bar` don't pop the picker.
  if (colonIdx > 0) {
    const prev = text[colonIdx - 1]
    if (prev && !/\s/.test(prev)) return null
  }
  const query = text.slice(colonIdx + 1)
  if (query.length < MIN_QUERY) return null
  if (!QUERY_RE.test(query)) return null
  return { query, openOffset: colonIdx }
}

function insertEmoji(view: EditorView, openOffset: number, emoji: string) {
  const { state } = view
  const { $from } = state.selection
  const start = $from.start() + openOffset
  const end = $from.pos
  view.dispatch(state.tr.insertText(emoji, start, end).scrollIntoView())
  view.focus()
}

interface PickerState {
  visible: boolean
  query: string
  openOffset: number
}

export function EmojiAutocompleteView() {
  const ref = useRef<HTMLDivElement>(null)
  // Escape latches a dismissed offset so the menu stays hidden for the current
  // `:token` while the user keeps typing or deletes it; cleared when the
  // trigger goes away or moves.
  const dismissedOffsetRef = useRef<number | null>(null)
  const { view } = usePluginViewContext()
  const [loading, getEditor] = useInstance()

  // The dataset loads lazily; flip this once it resolves so searchEmoji starts
  // returning hits (it no-ops until then) and the positioning effect re-runs.
  const [ready, setReady] = useState(false)
  useEffect(() => {
    let alive = true
    void ensureEmojiLoaded().then(() => {
      if (alive) setReady(true)
    })
    return () => {
      alive = false
    }
  }, [])

  const [{ visible, query, openOffset }, setPickerState] =
    useState<PickerState>({ visible: false, query: '', openOffset: -1 })
  const [activeIdx, setActiveIdx] = useState(0)

  const items = useMemo<EmojiHit[]>(
    () => (ready ? searchEmoji(query) : []),
    [query, ready],
  )

  // Move the menu out of the editor DOM so PM doesn't manage it (mirrors
  // SlashView). Positioning + show/hide is handled directly below.
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const parent = view.dom.parentElement
    if (parent && el.parentElement !== parent) parent.appendChild(el)
  }, [view])

  // Per-render: read the trigger from the live view, then show + position the
  // menu (or hide it). `ready` is in the dep list implicitly via re-render, so
  // when the dataset finishes loading this re-runs and the menu appears.
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const next = readEmojiState(view)
    const hits = next && ready ? searchEmoji(next.query) : []
    const dismissed =
      next != null && dismissedOffsetRef.current === next.openOffset
    if (next == null) dismissedOffsetRef.current = null
    const shouldShow =
      next != null &&
      !dismissed &&
      hits.length > 0 &&
      view.hasFocus() &&
      view.editable
    if (!shouldShow) {
      setShow(el, false)
      setPickerState((prev) =>
        prev.visible ? { visible: false, query: '', openOffset: -1 } : prev,
      )
      return
    }
    const coords = view.coordsAtPos(view.state.selection.from)
    setPos(el, coords.left, coords.bottom + 4)
    setShow(el, true)
    setPickerState((prev) =>
      prev.visible &&
      prev.query === next.query &&
      prev.openOffset === next.openOffset
        ? prev
        : { visible: true, query: next.query, openOffset: next.openOffset },
    )
    // Re-measure after paint to flip up / clamp inside the viewport.
    const rafId = requestAnimationFrame(() => {
      const r = el.getBoundingClientRect()
      const vh = window.innerHeight
      const vw = window.innerWidth
      let top = coords.bottom + 4
      if (top + r.height > vh && coords.top > vh - coords.bottom) {
        top = coords.top - r.height - 4
      }
      top = Math.max(4, Math.min(top, vh - r.height - 4))
      let left = coords.left
      if (left + r.width > vw) left = vw - r.width - 4
      left = Math.max(4, left)
      setPos(el, left, top)
    })
    return () => cancelAnimationFrame(rafId)
  })

  useEffect(() => {
    setActiveIdx((i) => (i >= items.length ? 0 : i))
  }, [items.length])

  useEffect(() => {
    setActiveIdx(0)
  }, [query])

  // Keep the active item within the scroll region as arrow-nav moves it.
  useEffect(() => {
    if (!visible) return
    const active = ref.current?.querySelector(
      '[aria-selected="true"]',
    ) as HTMLElement | null
    active?.scrollIntoView({ block: 'nearest', behavior: 'instant' })
  }, [activeIdx, visible])

  const runInsert = useCallback(
    (hit: EmojiHit) => {
      if (loading) return
      const el = ref.current
      if (el) setShow(el, false)
      setPickerState((prev) =>
        prev.visible ? { visible: false, query: '', openOffset: -1 } : prev,
      )
      const editor = getEditor()
      editor?.action((ctx) => {
        insertEmoji(ctx.get(editorViewCtx), openOffset, hit.emoji)
      })
    },
    [loading, getEditor, openOffset],
  )

  useEffect(() => {
    if (!visible) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        if (items.length === 0) return
        e.preventDefault()
        e.stopPropagation()
        setActiveIdx((i) => (i + 1) % items.length)
      } else if (e.key === 'ArrowUp') {
        if (items.length === 0) return
        e.preventDefault()
        e.stopPropagation()
        setActiveIdx((i) => (i - 1 + items.length) % items.length)
      } else if (e.key === 'Enter' || e.key === 'Tab') {
        const hit = items[activeIdx]
        if (!hit) return
        e.preventDefault()
        e.stopPropagation()
        runInsert(hit)
      } else if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        dismissedOffsetRef.current = openOffset
        const el = ref.current
        if (el) setShow(el, false)
        setPickerState({ visible: false, query: '', openOffset: -1 })
      }
    }
    document.addEventListener('keydown', onKey, true)
    return () => document.removeEventListener('keydown', onKey, true)
  }, [visible, items, activeIdx, runInsert, openOffset])

  return (
    <div
      ref={ref}
      role="listbox"
      aria-label="Insert emoji"
      className="tela-emoji-menu"
    >
      {items.map((hit, idx) => (
        <button
          key={hit.name}
          type="button"
          role="option"
          aria-selected={idx === activeIdx}
          data-active={idx === activeIdx ? 'true' : 'false'}
          className="tela-emoji-item"
          onMouseEnter={() => setActiveIdx(idx)}
          onMouseDown={(e) => {
            e.preventDefault()
            runInsert(hit)
          }}
        >
          <span className="tela-emoji-item-glyph">{hit.emoji}</span>
          <span className="tela-emoji-item-name">:{hit.name}:</span>
        </button>
      ))}
    </div>
  )
}
