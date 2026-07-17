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
import { useUsers, type MentionUser } from '../../lib/queries/users'
import type { PageListItem } from '../../lib/types'
import {
  disambiguateBreadcrumbs,
  type DisambiguatedRow,
} from '../../lib/disambiguateBreadcrumbs'

// Floating caret-anchored page autocomplete. Two triggers feed the same
// picker: `[[Page]]` (wiki-style) and `@Page` (mention-style) — both insert a
// `[Title](tela://page/{id})` link, so a page-mention IS a page link. Mirrors
// the slash plugin's shape (slashFactory + SlashProvider + capture-phase
// keydown for nav). Saved markdown round-trips via the stock commonmark link
// mark — M5.2a's `parseWikiLinks` regex picks those up to populate the
// `page_links` table on save. The `@` trigger ALSO lists people: picking one
// inserts `[Name](tela://user/{id})`, which the backend's userMentionRE parses
// on save to emit a `mention` notification (notifyPageMentions). Note that only
// PAGE bodies are mention-parsed — a mention in a comment notifies nobody.
// The companion decoration plugin
// lives in milkdown-wikilink-decoration.ts (separate file so react-refresh
// allows a non-component constant export alongside this React view).

// eslint-disable-next-line react-refresh/only-export-components -- milkdown plugin slice lives with its view
export const wikilinkPlugin = slashFactory('tela-wikilink')

// ---------- Trigger detection ------------------------------------------------

interface WikilinkTrigger {
  query: string
  // Offset of the trigger's first char within the parent paragraph — the `[`
  // of `[[` or the `@`. Insertion replaces from here to the cursor, so the
  // trigger text itself is consumed regardless of which form opened the picker.
  openOffset: number
  // `[[` → pages only; `@` → people + pages (Notion-style combined picker).
  trigger: 'bracket' | 'at'
}

// Block start OR whitespace before the trigger char — prevents mid-word
// triggers (and, for `@`, keeps `foo@bar.com` emails from popping the picker).
function triggerPrecededOk(text: string, idx: number): boolean {
  if (idx === 0) return true
  const prev = text[idx - 1]
  return !prev || /\s/.test(prev)
}

function readWikilinkState(view: EditorView): WikilinkTrigger | null {
  const { selection } = view.state
  const { empty, $from } = selection
  if (!empty) return null
  if ($from.parent.type.name !== 'paragraph') return null
  const text = $from.parent.textBetween(0, $from.parentOffset, undefined, '￼')

  const candidates: WikilinkTrigger[] = []

  // `[[` — whitespace inside the query is allowed (titles have spaces); any
  // `]` closes the trigger so we don't fight a user typing `]]` manually.
  const bracketIdx = text.lastIndexOf('[[')
  if (bracketIdx >= 0 && triggerPrecededOk(text, bracketIdx)) {
    const query = text.slice(bracketIdx + 2)
    if (!query.includes(']')) {
      candidates.push({ query, openOffset: bracketIdx, trigger: 'bracket' })
    }
  }

  // `@` — opens on a bare `@` and while typing a name. A leading space
  // (`@ foo`) means "not a mention" so prose `@` is left alone.
  const atIdx = text.lastIndexOf('@')
  if (atIdx >= 0 && triggerPrecededOk(text, atIdx)) {
    const query = text.slice(atIdx + 1)
    if (!/^\s/.test(query)) {
      candidates.push({ query, openOffset: atIdx, trigger: 'at' })
    }
  }

  if (candidates.length === 0) return null
  // If both are present, the one closest to the cursor (largest offset) wins.
  candidates.sort((a, b) => b.openOffset - a.openOffset)
  return candidates[0]
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

// Person mention — same shape as a wikilink but href `tela://user/{id}` and
// `@username` text. Round-trips as a stock link; the decoration renders a chip.
function insertUserMention(
  view: EditorView,
  openOffset: number,
  userId: number,
  username: string,
) {
  const { state } = view
  const { $from } = state.selection
  const start = $from.start() + openOffset
  const end = $from.pos
  const linkType = state.schema.marks.link
  if (!linkType) return
  const mark = linkType.create({ href: `tela://user/${userId}`, title: null })
  const linkText = state.schema.text(`@${username}`, [mark])
  const spaceText = state.schema.text(' ', [])
  view.dispatch(
    state.tr
      .replaceWith(start, end, [linkText, spaceText])
      .setStoredMarks([])
      .scrollIntoView(),
  )
  view.focus()
}

function filterUsers(users: MentionUser[], query: string): MentionUser[] {
  const q = query.trim().toLowerCase()
  if (q === '') return users
  return users.filter(
    (u) =>
      u.username.toLowerCase().includes(q) ||
      (u.email ?? '').toLowerCase().includes(q),
  )
}

// ---------- React picker view -----------------------------------------------

// A picker row is either a page (both triggers) or a person (only on `@`).
type PickerItem =
  | { kind: 'page'; row: PickerRow }
  | { kind: 'user'; user: MentionUser }

interface PickerState {
  visible: boolean
  query: string
  openOffset: number
  trigger: 'bracket' | 'at'
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
  const { data: users } = useUsers()

  const [{ visible, query, openOffset, trigger }, setPickerState] =
    useState<PickerState>({
      visible: false,
      query: '',
      openOffset: -1,
      trigger: 'bracket',
    })
  const [activeIdx, setActiveIdx] = useState(0)

  const rows = useMemo(() => disambiguateBreadcrumbs(pages ?? []), [pages])
  // `@` → people first, then pages; `[[` → pages only.
  const items = useMemo<PickerItem[]>(() => {
    const pageItems: PickerItem[] = filterRows(rows, query).map((row) => ({
      kind: 'page',
      row,
    }))
    if (trigger !== 'at') return pageItems
    const userItems: PickerItem[] = filterUsers(users ?? [], query).map(
      (user) => ({ kind: 'user', user }),
    )
    return [...userItems, ...pageItems]
  }, [rows, users, query, trigger])

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
        prev.visible
          ? { visible: false, query: '', openOffset: -1, trigger: 'bracket' }
          : prev,
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
      // eslint-disable-next-line react-hooks/set-state-in-effect -- syncs PM picker state, identity-guarded
      setPickerState((prev) =>
        prev.visible
          ? { visible: false, query: '', openOffset: -1, trigger: 'bracket' }
          : prev,
      )
      return
    }
    if (dismissedOffsetRef.current === next.openOffset) {
      // Still dismissed for this trigger position — keep React state hidden.
      setPickerState((prev) =>
        prev.visible
          ? { visible: false, query: '', openOffset: -1, trigger: 'bracket' }
          : prev,
      )
      return
    }
    // New or live trigger — clear any stale latch and mirror state.
    dismissedOffsetRef.current = null
    setPickerState((prev) =>
      prev.visible &&
      prev.query === next.query &&
      prev.openOffset === next.openOffset &&
      prev.trigger === next.trigger
        ? prev
        : {
            visible: true,
            query: next.query,
            openOffset: next.openOffset,
            trigger: next.trigger,
          },
    )
  })

  // Reset the highlighted row whenever the filtered list changes — protects
  // against an out-of-range index when the user narrows the query.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- clamps the active row when the list shrinks
    setActiveIdx((i) => (i >= items.length ? 0 : i))
  }, [items.length])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- resets the active row on query change
    setActiveIdx(0)
  }, [query])

  const runInsert = useCallback(
    (item: PickerItem) => {
      if (loading) return
      const editor = getEditor()
      editor?.action((ctx) => {
        const v = ctx.get(editorViewCtx)
        if (item.kind === 'user') {
          insertUserMention(v, openOffset, item.user.id, item.user.username)
        } else {
          insertWikilink(v, openOffset, item.row.item.id, item.row.item.title)
        }
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
        setPickerState({
          visible: false,
          query: '',
          openOffset: -1,
          trigger: 'bracket',
        })
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
      aria-label={trigger === 'at' ? 'Mention a person or page' : 'Link to page'}
      className="tela-wikilink-menu"
    >
      {items.length === 0 ? (
        <div className="tela-wikilink-empty">No matches.</div>
      ) : (
        items.map((item, idx) => {
          const key =
            item.kind === 'user'
              ? `user-${item.user.id}`
              : `page-${item.row.item.id}`
          return (
            <button
              key={key}
              type="button"
              role="option"
              aria-selected={idx === activeIdx}
              data-active={idx === activeIdx ? 'true' : 'false'}
              className="tela-wikilink-item"
              onMouseEnter={() => setActiveIdx(idx)}
              onMouseDown={(e) => {
                e.preventDefault()
                runInsert(item)
              }}
            >
              {item.kind === 'user' ? (
                <>
                  <span className="tela-wikilink-item-head">
                    <span className="tela-wikilink-item-title">
                      @{item.user.username}
                    </span>
                    <span className="tela-wikilink-item-chip">Person</span>
                  </span>
                  {item.user.email ? (
                    <span className="tela-wikilink-item-breadcrumb">
                      {item.user.email}
                    </span>
                  ) : null}
                </>
              ) : (
                <>
                  <span className="tela-wikilink-item-head">
                    <span className="tela-wikilink-item-title">
                      {item.row.item.title || 'Untitled'}
                    </span>
                    {item.row.showSpaceChip ? (
                      <span className="tela-wikilink-item-chip">
                        {item.row.item.space_name}
                      </span>
                    ) : null}
                  </span>
                  <span className="tela-wikilink-item-breadcrumb">
                    {item.row.breadcrumbLabel}
                  </span>
                </>
              )}
            </button>
          )
        })
      )}
    </div>
  )
}
