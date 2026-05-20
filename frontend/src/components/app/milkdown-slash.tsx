import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { slashFactory, SlashProvider } from '@milkdown/kit/plugin/slash'
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
import { insertExcalidraw } from './milkdown-excalidraw'

export const slashPlugin = slashFactory('tela-slash')

interface SlashCommand {
  id: string
  label: string
  hint: string
  keywords: string[]
  run: (ctx: Ctx) => void
}

const ALL_COMMANDS: SlashCommand[] = [
  {
    id: 'h1',
    label: 'Heading 1',
    hint: 'Large section heading',
    keywords: ['h1', 'heading 1', 'heading', 'title'],
    run: (ctx) => ctx.get(commandsCtx).call(wrapInHeadingCommand.key, 1),
  },
  {
    id: 'h2',
    label: 'Heading 2',
    hint: 'Medium section heading',
    keywords: ['h2', 'heading 2', 'heading', 'subtitle'],
    run: (ctx) => ctx.get(commandsCtx).call(wrapInHeadingCommand.key, 2),
  },
  {
    id: 'h3',
    label: 'Heading 3',
    hint: 'Small section heading',
    keywords: ['h3', 'heading 3', 'heading'],
    run: (ctx) => ctx.get(commandsCtx).call(wrapInHeadingCommand.key, 3),
  },
  {
    id: 'bullet-list',
    label: 'Bulleted list',
    hint: 'Unordered list',
    keywords: ['bullet', 'unordered', 'list', 'ul'],
    run: (ctx) => ctx.get(commandsCtx).call(wrapInBulletListCommand.key),
  },
  {
    id: 'ordered-list',
    label: 'Numbered list',
    hint: 'Ordered list',
    keywords: ['numbered', 'ordered', 'list', 'ol'],
    run: (ctx) => ctx.get(commandsCtx).call(wrapInOrderedListCommand.key),
  },
  {
    id: 'quote',
    label: 'Quote',
    hint: 'Block quote',
    keywords: ['quote', 'blockquote'],
    run: (ctx) => ctx.get(commandsCtx).call(wrapInBlockquoteCommand.key),
  },
  {
    id: 'collapsible',
    label: 'Collapsible',
    hint: 'Toggleable details block',
    keywords: ['collapsible', 'details', 'toggle', 'disclosure', 'expand'],
    run: insertCollapsible,
  },
  {
    id: 'excalidraw',
    label: 'Excalidraw',
    hint: 'Hand-drawn diagram',
    keywords: ['excalidraw', 'diagram', 'drawing', 'sketch', 'whiteboard'],
    run: insertExcalidraw,
  },
  {
    id: 'code',
    label: 'Code block',
    hint: 'Syntax-highlighted code',
    keywords: ['code', 'codeblock', 'pre'],
    run: (ctx) => ctx.get(commandsCtx).call(createCodeBlockCommand.key),
  },
  {
    id: 'divider',
    label: 'Divider',
    hint: 'Horizontal rule',
    keywords: ['hr', 'divider', 'separator', 'rule'],
    run: (ctx) => ctx.get(commandsCtx).call(insertHrCommand.key),
  },
  {
    id: 'table',
    label: 'Table',
    hint: '3 rows x 2 cols',
    keywords: ['table', 'grid'],
    run: (ctx) =>
      ctx.get(commandsCtx).call(insertTableCommand.key, { row: 3, col: 2 }),
  },
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
  const providerRef = useRef<SlashProvider | null>(null)
  const { view, prevState } = usePluginViewContext()
  const [loading, getEditor] = useInstance()

  const [{ visible, query }, setSlashState] = useState<SlashState>({
    visible: false,
    query: '',
  })
  const [activeIdx, setActiveIdx] = useState(0)

  const items = useMemo(() => filterCommands(query), [query])

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const provider = new SlashProvider({
      content: el,
      shouldShow(v) {
        if (!v.hasFocus()) return false
        if (!v.editable) return false
        return readSlashState(v) != null
      },
      floatingUIOptions: { strategy: 'fixed' },
    })
    providerRef.current = provider
    return () => {
      provider.destroy()
      providerRef.current = null
    }
  }, [])

  // Run on every editor update. shouldShow gates visibility; we also mirror
  // the query into React state so the list can filter.
  useEffect(() => {
    providerRef.current?.update(view, prevState)
    const next = readSlashState(view)
    if (next) {
      setSlashState((prev) =>
        prev.visible && prev.query === next.query
          ? prev
          : { visible: true, query: next.query },
      )
    } else {
      setSlashState((prev) =>
        prev.visible ? { visible: false, query: '' } : prev,
      )
    }
  })

  useEffect(() => {
    setActiveIdx(0)
  }, [query, items.length])

  const runCommand = useCallback(
    (cmd: SlashCommand) => {
      if (loading) return
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
