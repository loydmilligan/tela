import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { slashFactory } from '@milkdown/kit/plugin/slash'
import { usePluginViewContext } from '@prosemirror-adapter/react'
import { useInstance } from '@milkdown/react'
import { commandsCtx, editorViewCtx } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import { TextSelection } from '@milkdown/kit/prose/state'
import {
  createCodeBlockCommand,
  insertHrCommand,
  wrapInBlockquoteCommand,
  wrapInBulletListCommand,
  wrapInHeadingCommand,
  wrapInOrderedListCommand,
} from '@milkdown/kit/preset/commonmark'
import { insertTableCommand } from '@milkdown/kit/preset/gfm'
import { COLLAPSIBLE_DEFAULT_SUMMARY } from './milkdown-collapsibles'
import { insertCallout } from './milkdown-callouts'
import { insertExcalidraw } from './milkdown-excalidraw'
import { insertTaskList } from './milkdown-task-list'
import { insertMathBlock } from './milkdown-math'
import { TEMPLATES, insertTemplate } from './milkdown-templates'
import { setPos, setShow } from './milkdown-floating'
import { insertMermaid } from './milkdown-mermaid'
import { insertChart } from './milkdown-chart'
import { insertTabs } from './milkdown-tabs'
import { insertKanban } from './milkdown-kanban'
import { insertStatGrid } from './milkdown-stat-grid'
import { insertTimeline } from './milkdown-timeline'
import { insertCalendar } from './milkdown-calendar'
import { insertPullquote } from './milkdown-pullquote'
import { insertEmbed } from './milkdown-embed'
import { openEmojiPicker } from './milkdown-emoji'
import { SLASH_BLOCKS } from './blocks-manifest'

export const slashPlugin = slashFactory('tela-slash')

interface SlashCommand {
  id: string
  label: string
  hint: string
  keywords: string[]
  run: (ctx: Ctx) => void
}

// Behaviour, keyed by manifest block id. The labels/hints/keywords/ordering all
// live in blocks-manifest.json (the source of truth shared with the agent
// authoring guide); this map only supplies the imperative insert per id, so a
// new block needs an entry in both. The integrity check below fails loudly in
// dev if the two ever drift.
const RUN: Record<string, (ctx: Ctx) => void> = {
  h1: (ctx) => ctx.get(commandsCtx).call(wrapInHeadingCommand.key, 1),
  h2: (ctx) => ctx.get(commandsCtx).call(wrapInHeadingCommand.key, 2),
  h3: (ctx) => ctx.get(commandsCtx).call(wrapInHeadingCommand.key, 3),
  'bullet-list': (ctx) => ctx.get(commandsCtx).call(wrapInBulletListCommand.key),
  'ordered-list': (ctx) => ctx.get(commandsCtx).call(wrapInOrderedListCommand.key),
  'task-list': insertTaskList,
  quote: (ctx) => ctx.get(commandsCtx).call(wrapInBlockquoteCommand.key),
  'pull-quote': insertPullquote,
  callout: insertCallout,
  collapsible: insertCollapsible,
  excalidraw: insertExcalidraw,
  code: (ctx) => ctx.get(commandsCtx).call(createCodeBlockCommand.key),
  divider: (ctx) => ctx.get(commandsCtx).call(insertHrCommand.key),
  table: (ctx) =>
    ctx.get(commandsCtx).call(insertTableCommand.key, { row: 3, col: 2 }),
  equation: insertMathBlock,
  mermaid: insertMermaid,
  chart: insertChart,
  embed: insertEmbed,
  tabs: insertTabs,
  kanban: insertKanban,
  'stat-grid': insertStatGrid,
  timeline: insertTimeline,
  calendar: insertCalendar,
  emoji: openEmojiPicker,
  date: (ctx) => {
    const view = ctx.get(editorViewCtx)
    // YYYY-MM-DD, matching the project's date convention.
    const today = new Date().toISOString().slice(0, 10)
    view.dispatch(view.state.tr.insertText(today))
    view.focus()
  },
}

// Fail fast in dev if the manifest's slash blocks and this behaviour map drift:
// every slash block needs a `run`, and no `run` should reference a block the
// manifest doesn't mark slash-insertable.
if (import.meta.env?.DEV) {
  const ids = new Set(SLASH_BLOCKS.map((b) => b.id))
  const missing = SLASH_BLOCKS.filter((b) => !RUN[b.id]).map((b) => b.id)
  const orphan = Object.keys(RUN).filter((id) => !ids.has(id))
  if (missing.length || orphan.length) {
    throw new Error(
      `slash menu / blocks-manifest drift — missing run: [${missing}], orphan run: [${orphan}]`,
    )
  }
}

const ALL_COMMANDS: SlashCommand[] = [
  // Block items projected from the manifest (labels/hints/keywords/order live
  // there); RUN supplies the insert behaviour per id.
  ...SLASH_BLOCKS.map(
    (b): SlashCommand => ({
      id: b.id,
      label: b.label,
      hint: b.hint,
      keywords: b.keywords,
      run: RUN[b.id],
    }),
  ),
  // Built-in templates (markdown snippets). See milkdown-templates.ts.
  ...TEMPLATES.map(
    (t): SlashCommand => ({
      id: t.id,
      label: t.label,
      hint: 'Template',
      keywords: t.keywords,
      run: (ctx) => insertTemplate(ctx, t.markdown),
    }),
  ),
]

interface SlashState {
  visible: boolean
  query: string
}

// Inspect the current selection and return the slash query if the menu should
// be active, or null otherwise.
function readSlashState(view: ReturnType<typeof usePluginViewContext>['view']):
  | { query: string }
  | null {
  const { selection } = view.state
  const { empty, $from } = selection
  if (!empty) return null
  if ($from.parent.type.name !== 'paragraph') return null
  const text = $from.parent.textBetween(0, $from.parentOffset, undefined, '￼')
  const slashIdx = text.lastIndexOf('/')
  if (slashIdx < 0) return null
  // The `/` must be at the start of the block OR preceded by whitespace, so a
  // slash mid-word doesn't pop the menu.
  if (slashIdx > 0) {
    const prev = text[slashIdx - 1]
    if (prev && !/\s/.test(prev)) return null
  }
  const after = text.slice(slashIdx + 1)
  if (/\s/.test(after)) return null
  return { query: after }
}

function filterCommands(query: string): SlashCommand[] {
  const q = query.trim().toLowerCase()
  if (!q) return ALL_COMMANDS
  return ALL_COMMANDS.filter(
    (c) =>
      c.label.toLowerCase().includes(q) ||
      c.keywords.some((k) => k.toLowerCase().includes(q)),
  )
}

// Insert a fresh `details > details_summary("Click to expand") + paragraph("")`
// node at the cursor and land the caret inside the empty body paragraph so
// the user can immediately start typing. The replacement uses
// `replaceSelectionWith`, which has subtle positioning semantics (replaces an
// empty enclosing textblock; splits non-empty; appends at the closest valid
// level otherwise) — instead of computing the new details' start position
// arithmetically, we walk the post-replacement doc for a details node whose
// summary equals our placeholder. Robust to PM's positioning quirks at the
// cost of one O(doc) walk (negligible).
function insertCollapsible(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const { state } = view
  const detailsType = state.schema.nodes.details
  const summaryType = state.schema.nodes.details_summary
  const paragraphType = state.schema.nodes.paragraph
  if (!detailsType || !summaryType || !paragraphType) return
  const summary = summaryType.create(
    null,
    state.schema.text(COLLAPSIBLE_DEFAULT_SUMMARY),
  )
  const body = paragraphType.create()
  const details = detailsType.create(null, [summary, body])
  const tr = state.tr.replaceSelectionWith(details)
  // Find the LAST details whose summary matches the placeholder — assumed to be
  // the one we just inserted. (A user can't realistically have a pre-existing
  // details with the exact placeholder text below the insertion point during
  // the same tick; even if they do, the cursor still lands inside a valid
  // collapsible body and the worst case is a one-keystroke nuisance.)
  let targetPos = -1
  tr.doc.descendants((node, pos) => {
    if (node.type === detailsType) {
      const sm = node.firstChild
      if (sm && sm.textContent === COLLAPSIBLE_DEFAULT_SUMMARY) {
        targetPos = pos
      }
    }
    return true
  })
  if (targetPos !== -1) {
    // Inside the inserted details, the body paragraph sits right after the
    // summary. Caret = detailsStart + 1 (enter details) + summary.nodeSize
    // (skip summary) + 1 (enter body paragraph).
    const caret = targetPos + 1 + summary.nodeSize + 1
    tr.setSelection(TextSelection.create(tr.doc, caret))
  }
  view.dispatch(tr.scrollIntoView())
  view.focus()
}

// Delete the `/query` text in the current paragraph before running a command.
function clearSlashTrigger(ctx: Ctx) {
  const view = ctx.get(editorViewCtx)
  const { state } = view
  const { $from } = state.selection
  const text = $from.parent.textBetween(0, $from.parentOffset, undefined, '￼')
  const slashIdx = text.lastIndexOf('/')
  if (slashIdx < 0) return
  const start = $from.start() + slashIdx
  view.dispatch(state.tr.delete(start, $from.pos))
}

export function SlashView() {
  const ref = useRef<HTMLDivElement>(null)
  const { view } = usePluginViewContext()
  const [loading, getEditor] = useInstance()

  const [{ visible, query }, setSlashState] = useState<SlashState>({
    visible: false,
    query: '',
  })
  const [activeIdx, setActiveIdx] = useState(0)

  const items = useMemo(() => filterCommands(query), [query])

  // Move the menu out of the editor DOM so PM doesn't manage it, mirroring
  // what SlashProvider would do on its first update() call. We don't use
  // SlashProvider here because its internal lodash.debounce reliably gets
  // wedged in our React + Yjs + Vite setup (the 200ms timer keeps resetting
  // and #onUpdate never fires past the initial appendChild), leaving the
  // menu hidden after every `/` press. Positioning + show/hide is simple
  // enough to manage directly via view.coordsAtPos.
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const parent = view.dom.parentElement
    if (parent && el.parentElement !== parent) {
      parent.appendChild(el)
    }
  }, [view])

  // Per-render: read slash state from the live view, then show + position
  // the menu (or hide it). Position is computed from view.coordsAtPos so
  // the menu lands directly below the cursor; a follow-up rAF re-measures
  // and flips upward if the menu would overflow the bottom of the viewport.
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const next = readSlashState(view)
    const shouldShow = next != null && view.hasFocus() && view.editable
    if (!shouldShow) {
      setShow(el, false)
      setSlashState((prev) =>
        prev.visible ? { visible: false, query: '' } : prev,
      )
      return
    }
    const { from } = view.state.selection
    const coords = view.coordsAtPos(from)
    setPos(el, coords.left, coords.bottom + 4)
    setShow(el, true)
    setSlashState((prev) =>
      prev.visible && prev.query === next.query
        ? prev
        : { visible: true, query: next.query },
    )
    // Re-measure after paint so we can flip / clamp if the rendered menu
    // would overflow the viewport. Without this, opening near the bottom
    // of the page can leave the menu partially off-screen.
    const rafId = requestAnimationFrame(() => {
      const r = el.getBoundingClientRect()
      const vh = window.innerHeight
      const vw = window.innerWidth
      let top = coords.bottom + 4
      // Prefer below; if it overflows bottom AND there's more room above,
      // flip up. Then clamp so the menu always stays inside the viewport.
      if (top + r.height > vh && coords.top > vh - coords.bottom) {
        top = coords.top - r.height - 4
      }
      top = Math.max(4, Math.min(top, vh - r.height - 4))
      let left = coords.left
      if (left + r.width > vw) {
        left = vw - r.width - 4
      }
      left = Math.max(4, left)
      setPos(el, left, top)
    })
    return () => cancelAnimationFrame(rafId)
  })

  useEffect(() => {
    setActiveIdx(0)
  }, [query, items.length])

  // Keep the active item in view when arrow-nav moves the selection past
  // the menu's max-height scroll region. 'instant' so rapid arrow-down
  // doesn't visually lag the selection.
  useEffect(() => {
    if (!visible) return
    const container = ref.current
    if (!container) return
    const active = container.querySelector(
      '[aria-selected="true"]',
    ) as HTMLElement | null
    active?.scrollIntoView({ block: 'nearest', behavior: 'instant' })
  }, [activeIdx, visible])

  const runCommand = useCallback(
    (cmd: SlashCommand) => {
      if (loading) return
      // Hide the menu immediately so it doesn't visibly reposition through the
      // multi-step insert transaction before the trigger-clear hides it.
      const el = ref.current
      if (el) setShow(el, false)
      setSlashState((prev) => (prev.visible ? { visible: false, query: '' } : prev))
      const editor = getEditor()
      editor?.action((ctx) => {
        clearSlashTrigger(ctx)
        cmd.run(ctx)
      })
    },
    [loading, getEditor],
  )

  // Capture-phase keydown so we beat ProseMirror's handler when the menu is
  // open. Arrow keys nav within the list; Enter selects.
  useEffect(() => {
    if (!visible) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        e.stopPropagation()
        setActiveIdx((i) => (items.length === 0 ? 0 : (i + 1) % items.length))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        e.stopPropagation()
        setActiveIdx((i) =>
          items.length === 0 ? 0 : (i - 1 + items.length) % items.length,
        )
      } else if (e.key === 'Enter') {
        const cmd = items[activeIdx]
        if (!cmd) return
        e.preventDefault()
        e.stopPropagation()
        runCommand(cmd)
      }
    }
    document.addEventListener('keydown', onKey, true)
    return () => document.removeEventListener('keydown', onKey, true)
  }, [visible, items, activeIdx, runCommand])

  return (
    <div
      ref={ref}
      role="listbox"
      aria-label="Insert block"
      className="tela-slash-menu"
    >
      {items.length === 0 ? (
        <div className="tela-slash-empty">No matches</div>
      ) : (
        items.map((cmd, idx) => (
          <button
            key={cmd.id}
            type="button"
            role="option"
            aria-selected={idx === activeIdx}
            data-active={idx === activeIdx ? 'true' : 'false'}
            className="tela-slash-item"
            onMouseEnter={() => setActiveIdx(idx)}
            onMouseDown={(e) => {
              e.preventDefault()
              runCommand(cmd)
            }}
          >
            <span className="tela-slash-item-label">{cmd.label}</span>
            <span className="tela-slash-item-hint">{cmd.hint}</span>
          </button>
        ))
      )}
    </div>
  )
}
