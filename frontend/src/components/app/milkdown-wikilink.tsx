import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { slashFactory, SlashProvider } from '@milkdown/kit/plugin/slash'
import { usePluginViewContext } from '@prosemirror-adapter/react'
import { useInstance } from '@milkdown/react'
import { editorViewCtx } from '@milkdown/kit/core'
import type { EditorView } from '@milkdown/kit/prose/view'
import { useAllPages } from '../../lib/queries/pages'
import type { PageListItem } from '../../lib/types'
import {
  disambiguateBreadcrumbs,
  type DisambiguatedRow,
} from '../../lib/disambiguateBreadcrumbs'

// Floating caret-anchored `[[Page]]` autocomplete. Mirrors the slash plugin's
// shape (slashFactory + SlashProvider + capture-phase keydown for nav). Saved
// markdown round-trips as `[Title](tela://page/{id})` via the stock commonmark
// link mark — M5.2a's `parseWikiLinks` regex already picks those up to
// populate the `page_links` table on save. The companion decoration plugin
// lives in milkdown-wikilink-decoration.ts (separate file so react-refresh
// allows a non-component constant export alongside this React view).

export const wikilinkPlugin = slashFactory('tela-wikilink')

// ---------- Trigger detection ------------------------------------------------

interface WikilinkTrigger {
  query: string
  openOffset: number // offset of the first `[` within the parent paragraph
}

function readWikilinkState(view: EditorView): WikilinkTrigger | null {
  const { selection } = view.state
  const { empty, $from } = selection
  if (!empty) return null
  if ($from.parent.type.name !== 'paragraph') return null
  const text = $from.parent.textBetween(0, $from.parentOffset, undefined, '￼')
  const openIdx = text.lastIndexOf('[[')
  if (openIdx < 0) return null
  // Block start OR whitespace before `[[`. Mirrors the slash plugin's rule
  // that prevents mid-word triggers.
  if (openIdx > 0) {
    const prev = text[openIdx - 1]
    if (prev && !/\s/.test(prev)) return null
  }
  const query = text.slice(openIdx + 2)
  // Any `]` after the `[[` closes the trigger — don't fight the user typing
  // `]]` manually. Whitespace inside the query is allowed (titles have spaces).
  if (query.includes(']')) return null
  return { query, openOffset: openIdx }
}

// ---------- Filtering --------------------------------------------------------

type PickerRow = DisambiguatedRow<PageListItem>

function filterRows(rows: PickerRow[], query: string): PickerRow[] {
  const q = query.trim().toLowerCase()
  if (q === '') return rows
  // Simple substring match against title + space + breadcrumb tokens. With
  // the v0 <100-pages ceiling baked into `/api/pages/all`, this stays
  // comfortably under a millisecond and keeps the picker dependency-free.
  return rows.filter((r) => {
    const haystack = [
      r.item.title,
      r.item.space_name,
      ...r.item.breadcrumb,
    ]
      .join(' ')
      .toLowerCase()
    return haystack.includes(q)
  })
}

// ---------- Insertion --------------------------------------------------------

function insertWikilink(
  view: EditorView,
  openOffset: number,
  pageId: number,
  title: string,
) {
  const { state } = view
  const { $from } = state.selection
  const start = $from.start() + openOffset
  const end = $from.pos
  const linkType = state.schema.marks.link
  if (!linkType) return
  const mark = linkType.create({ href: `tela://page/${pageId}`, title: null })
  const linkText = state.schema.text(title, [mark])
  // Trailing space without the link mark — a clean boundary so the user can
  // keep typing without inheriting the link mark on subsequent characters.
  const spaceText = state.schema.text(' ', [])
  const tr = state.tr
    .replaceWith(start, end, [linkText, spaceText])
    .setStoredMarks([])
    .scrollIntoView()
  view.dispatch(tr)
  view.focus()
}

// ---------- React picker view -----------------------------------------------

interface PickerState {
  visible: boolean
  query: string
  openOffset: number
}

export function WikilinkView() {
  const ref = useRef<HTMLDivElement>(null)
  const providerRef = useRef<SlashProvider | null>(null)
  // Escape latches a dismissed-offset; while the active trigger still starts
  // at that offset, the picker stays hidden so the user can keep typing or
  // delete the `[[…` manually. A new `[[` token at a different offset (or
  // dropping the trigger entirely) clears the latch.
  const dismissedOffsetRef = useRef<number | null>(null)
  const { view, prevState } = usePluginViewContext()
  const [loading, getEditor] = useInstance()

  const { data: pages } = useAllPages()

  const [{ visible, query, openOffset }, setPickerState] = useState<PickerState>(
    { visible: false, query: '', openOffset: -1 },
  )
  const [activeIdx, setActiveIdx] = useState(0)

  const rows = useMemo(
    () => disambiguateBreadcrumbs(pages ?? []),
    [pages],
  )
  const items = useMemo(() => filterRows(rows, query), [rows, query])

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const provider = new SlashProvider({
      content: el,
      shouldShow(v) {
        if (!v.hasFocus()) return false
        if (!v.editable) return false
        const trigger = readWikilinkState(v)
        if (!trigger) return false
        if (dismissedOffsetRef.current === trigger.openOffset) return false
        return true
      },
      floatingUIOptions: { strategy: 'fixed' },
    })
    providerRef.current = provider
    return () => {
      provider.destroy()
      providerRef.current = null
    }
  }, [])

  // Outside-click / focus-leave dismissal. Row mousedown handlers call
  // preventDefault(), so picking a row never blurs the editor and never
  // trips this listener. Anything else (sidebar click, breadcrumb click,
  // tab away) blurs the editor's contenteditable → hide the picker.
  useEffect(() => {
    const dom = view.dom
    const onBlur = () => {
      providerRef.current?.hide()
      setPickerState((prev) =>
        prev.visible ? { visible: false, query: '', openOffset: -1 } : prev,
      )
    }
    dom.addEventListener('blur', onBlur)
    return () => dom.removeEventListener('blur', onBlur)
  }, [view])

  // Mirror trigger state into React on every editor update; SlashProvider
  // toggles `data-show` based on the same predicate. The dismissed-offset
  // latch is cleared whenever the trigger goes away — so the next `[[` token
  // at any offset re-arms the picker.
  useEffect(() => {
    providerRef.current?.update(view, prevState)
    const next = readWikilinkState(view)
    if (next == null) {
      dismissedOffsetRef.current = null
      setPickerState((prev) =>
        prev.visible ? { visible: false, query: '', openOffset: -1 } : prev,
      )
      return
    }
    if (dismissedOffsetRef.current === next.openOffset) {
      // Still dismissed for this trigger position — keep React state hidden.
      setPickerState((prev) =>
        prev.visible ? { visible: false, query: '', openOffset: -1 } : prev,
      )
      return
    }
    // New or live trigger — clear any stale latch and mirror state.
    dismissedOffsetRef.current = null
    setPickerState((prev) =>
      prev.visible &&
      prev.query === next.query &&
      prev.openOffset === next.openOffset
        ? prev
        : { visible: true, query: next.query, openOffset: next.openOffset },
    )
  })

  // Reset the highlighted row whenever the filtered list changes — protects
  // against an out-of-range index when the user narrows the query.
  useEffect(() => {
    setActiveIdx((i) => (i >= items.length ? 0 : i))
  }, [items.length])

  useEffect(() => {
    setActiveIdx(0)
  }, [query])

  const runInsert = useCallback(
    (row: PickerRow) => {
      if (loading) return
      const editor = getEditor()
      editor?.action((ctx) => {
        const v = ctx.get(editorViewCtx)
        insertWikilink(v, openOffset, row.item.id, row.item.title)
      })
    },
    [loading, getEditor, openOffset],
  )

  // Capture-phase keydown — beats ProseMirror's handler so arrow nav and
  // Enter drive the picker without leaking into the editor. Escape only
  // dismisses the picker view; the `[[query` text stays in the editor for
  // the user to keep typing or delete manually.
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
      } else if (e.key === 'Enter') {
        const row = items[activeIdx]
        if (!row) return
        e.preventDefault()
        e.stopPropagation()
        runInsert(row)
      } else if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        dismissedOffsetRef.current = openOffset
        setPickerState({ visible: false, query: '', openOffset: -1 })
        providerRef.current?.hide()
      }
    }
    document.addEventListener('keydown', onKey, true)
    return () => document.removeEventListener('keydown', onKey, true)
  }, [visible, items, activeIdx, runInsert, openOffset])

  return (
    <div
      ref={ref}
      role="listbox"
      aria-label="Link to page"
      className="tela-wikilink-menu"
    >
      {items.length === 0 ? (
        <div className="tela-wikilink-empty">No matches.</div>
      ) : (
        items.map((row, idx) => (
          <button
            key={row.item.id}
            type="button"
            role="option"
            aria-selected={idx === activeIdx}
            data-active={idx === activeIdx ? 'true' : 'false'}
            className="tela-wikilink-item"
            onMouseEnter={() => setActiveIdx(idx)}
            onMouseDown={(e) => {
              e.preventDefault()
              runInsert(row)
            }}
          >
            <span className="tela-wikilink-item-head">
              <span className="tela-wikilink-item-title">
                {row.item.title || 'Untitled'}
              </span>
              {row.showSpaceChip ? (
                <span className="tela-wikilink-item-chip">
                  {row.item.space_name}
                </span>
              ) : null}
            </span>
            <span className="tela-wikilink-item-breadcrumb">
              {row.breadcrumbLabel}
            </span>
          </button>
        ))
      )}
    </div>
  )
}
