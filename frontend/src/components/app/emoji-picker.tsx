import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import {
  ensureEmojiLoaded,
  emojiGroups,
  searchEmoji,
  type EmojiEntry,
} from '../../lib/emoji'
import { Input } from '../ui/input'

// Visual `/`-triggered emoji picker: a caret-anchored popover with a search box
// over a category-grouped grid. Selecting inserts the Unicode emoji at the
// caret (the host bridges `onSelect` to insertEmojiAt — see milkdown-emoji.ts).
// Not `milkdown-`prefixed on purpose so it sits outside the blocks-gate plugin
// scan (same as excalidraw-edit-sheet.tsx).
//
// Positioning mirrors milkdown-mira-paste-popover.tsx: position:fixed, JS-set
// left/top from the captured caret coords, one rAF re-measure to flip up /
// clamp inside the viewport. Dismiss on Escape (capture phase) + outside click.

export interface EmojiPickerProps {
  anchor: { left: number; top: number; bottom: number }
  onSelect: (emoji: string) => void
  onClose: () => void
}

// How many search hits to show before the grid scrolls. The grouped (no-query)
// view shows everything; search is the high-traffic path so we cap it.
const SEARCH_LIMIT = 64

export function EmojiPicker({ anchor, onSelect, onClose }: EmojiPickerProps) {
  const rootRef = useRef<HTMLDivElement>(null)
  const [ready, setReady] = useState(false)
  const [query, setQuery] = useState('')

  useEffect(() => {
    let alive = true
    void ensureEmojiLoaded().then(() => {
      if (alive) setReady(true)
    })
    return () => {
      alive = false
    }
  }, [])

  const groups = useMemo(
    () => (ready ? emojiGroups() : []),
    [ready],
  )
  const hits = useMemo(
    () => (ready && query.trim() ? searchEmoji(query, SEARCH_LIMIT) : null),
    [ready, query],
  )

  // Initial placement before paint, then one rAF flip/clamp — identical to the
  // mira paste popover so editor popovers behave consistently.
  useLayoutEffect(() => {
    const el = rootRef.current
    if (!el) return
    el.style.left = `${anchor.left}px`
    el.style.top = `${anchor.bottom + 4}px`
    const rafId = requestAnimationFrame(() => {
      const r = el.getBoundingClientRect()
      const vh = window.innerHeight
      const vw = window.innerWidth
      let top = anchor.bottom + 4
      if (top + r.height > vh && anchor.top > vh - anchor.bottom) {
        top = anchor.top - r.height - 4
      }
      top = Math.max(4, Math.min(top, vh - r.height - 4))
      let left = anchor.left
      if (left + r.width > vw) left = vw - r.width - 4
      left = Math.max(4, left)
      el.style.top = `${top}px`
      el.style.left = `${left}px`
    })
    return () => cancelAnimationFrame(rafId)
  }, [anchor])

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        onClose()
      }
    }
    document.addEventListener('keydown', onKey, true)
    return () => document.removeEventListener('keydown', onKey, true)
  }, [onClose])

  useEffect(() => {
    function onDown(e: PointerEvent) {
      const root = rootRef.current
      if (!root) return
      if (e.target instanceof Node && root.contains(e.target)) return
      onClose()
    }
    document.addEventListener('pointerdown', onDown, true)
    return () => document.removeEventListener('pointerdown', onDown, true)
  }, [onClose])

  const renderButton = (e: EmojiEntry) => (
    <button
      key={e.name}
      type="button"
      className="tela-emoji-picker-cell"
      title={`:${e.name}:`}
      aria-label={e.name}
      onMouseDown={(ev) => {
        ev.preventDefault()
        onSelect(e.emoji)
      }}
    >
      {e.emoji}
    </button>
  )

  return (
    <div
      ref={rootRef}
      role="dialog"
      aria-label="Insert emoji"
      className="tela-emoji-picker"
      data-show="true"
    >
      <div className="tela-emoji-picker-search">
        <Input
          autoFocus
          size="sm"
          placeholder="Search emoji…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>
      <div className="tela-emoji-picker-body">
        {!ready ? (
          <p className="tela-emoji-picker-empty">Loading…</p>
        ) : hits ? (
          hits.length === 0 ? (
            <p className="tela-emoji-picker-empty">No emoji found.</p>
          ) : (
            <div className="tela-emoji-picker-grid">
              {hits.map((h) =>
                renderButton({
                  emoji: h.emoji,
                  name: h.name,
                  names: [h.name],
                  tags: [],
                  category: '',
                }),
              )}
            </div>
          )
        ) : (
          groups.map((g) => (
            <section key={g.category} className="tela-emoji-picker-group">
              <h3 className="tela-emoji-picker-group-label">{g.category}</h3>
              <div className="tela-emoji-picker-grid">
                {g.items.map(renderButton)}
              </div>
            </section>
          ))
        )}
      </div>
    </div>
  )
}
