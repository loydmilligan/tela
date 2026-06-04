import { useEffect, useRef, useState } from 'react'
import { tooltipFactory } from '@milkdown/kit/plugin/tooltip'
import { usePluginViewContext } from '@prosemirror-adapter/react'
import { useInstance } from '@milkdown/react'
import { commandsCtx, editorViewCtx } from '@milkdown/kit/core'
import type { CmdKey } from '@milkdown/kit/core'
import type { Ctx } from '@milkdown/ctx'
import { NodeSelection } from '@milkdown/kit/prose/state'
import type { EditorState } from '@milkdown/kit/prose/state'
import type { EditorView } from '@milkdown/kit/prose/view'
import {
  toggleEmphasisCommand,
  toggleInlineCodeCommand,
  toggleLinkCommand,
  toggleStrongCommand,
  updateLinkCommand,
} from '@milkdown/kit/preset/commonmark'
import { toggleStrikethroughCommand } from '@milkdown/kit/preset/gfm'
import { toggleHighlightCommand } from './milkdown-highlight'
import {
  Bold,
  Code,
  Highlighter,
  Italic,
  Link as LinkIcon,
  Strikethrough,
} from 'lucide-react'
import { Input } from '../ui/input'
import { cn } from '../../lib/utils'

export const bubblePlugin = tooltipFactory('tela-bubble')

// Selection bubble-toolbar. Appears above a non-empty text selection and lets
// the user apply inline marks (bold / italic / code / strikethrough / link)
// without knowing the markdown syntax — the Medium/Notion gesture. All five
// marks already exist in the schema (commonmark + gfm), so this is pure UI
// over existing commands; nothing about the canonical markdown changes.
//
// Like SlashView, we DON'T use Milkdown's TooltipProvider helper: its internal
// lodash.debounce wedges under our React + Yjs + Vite setup (same failure mode
// documented on the slash menu). Show/hide + positioning is managed directly
// from a no-dependency effect that re-reads the live view on every render.

interface ActiveMarks {
  strong: boolean
  emphasis: boolean
  inlineCode: boolean
  strike: boolean
  highlight: boolean
  link: boolean
}

const NO_MARKS: ActiveMarks = {
  strong: false,
  emphasis: false,
  inlineCode: false,
  strike: false,
  highlight: false,
  link: false,
}

function rangeHasMark(state: EditorState, markName: string): boolean {
  const type = state.schema.marks[markName]
  if (!type) return false
  const { from, to } = state.selection
  return state.doc.rangeHasMark(from, to, type)
}

function computeActive(state: EditorState): ActiveMarks {
  return {
    strong: rangeHasMark(state, 'strong'),
    emphasis: rangeHasMark(state, 'emphasis'),
    inlineCode: rangeHasMark(state, 'inlineCode'),
    strike: rangeHasMark(state, 'strike_through'),
    highlight: rangeHasMark(state, 'highlight'),
    link: rangeHasMark(state, 'link'),
  }
}

function sameActive(a: ActiveMarks, b: ActiveMarks): boolean {
  return (
    a.strong === b.strong &&
    a.emphasis === b.emphasis &&
    a.inlineCode === b.inlineCode &&
    a.strike === b.strike &&
    a.highlight === b.highlight &&
    a.link === b.link
  )
}

// The href on the first link mark inside the selection (for prefilling the
// edit field when the selection already sits on a link).
function currentLinkHref(state: EditorState): string {
  const type = state.schema.marks.link
  if (!type) return ''
  const { from, to } = state.selection
  let href = ''
  state.doc.nodesBetween(from, to, (node) => {
    const mark = node.marks.find((m) => m.type === type)
    if (mark) href = (mark.attrs.href as string) ?? ''
  })
  return href
}

function shouldShow(view: EditorView): boolean {
  if (!view.editable || !view.hasFocus()) return false
  const sel = view.state.selection
  if (sel.empty) return false
  // A node selection (e.g. an image/excalidraw atom) isn't a text range.
  if (sel instanceof NodeSelection) return false
  // Marks are meaningless inside a code block (`spec.code`); don't offer them.
  // (`$from.parent` is the block at the selection start; for a multi-block /
  // select-all range this still correctly skips a code-block-only selection.)
  if (sel.$from.parent.type.spec.code) return false
  return true
}

export function BubbleToolbarView() {
  const ref = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const { view } = usePluginViewContext()
  const [loading, getEditor] = useInstance()

  const [active, setActive] = useState<ActiveMarks>(NO_MARKS)
  // When set, the toolbar shows a URL field instead of the mark buttons. It
  // stays open while the field is focused (the editor blurs, but the PM
  // selection persists, so the applied mark lands on the right range).
  const [linkMode, setLinkMode] = useState(false)
  const [linkValue, setLinkValue] = useState('')

  // Reparent out of the editor DOM so PM doesn't manage the node (mirrors
  // SlashView). Done once per view.
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const parent = view.dom.parentElement
    if (parent && el.parentElement !== parent) parent.appendChild(el)
  }, [view])

  // No-dependency effect: runs after every render, re-reads the live view, and
  // shows + positions the toolbar above the selection (or hides it). While the
  // link field is open we keep it visible regardless of focus/selection.
  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (linkMode) {
      el.dataset.show = 'true'
      return
    }
    if (!shouldShow(view)) {
      el.dataset.show = 'false'
      return
    }
    el.dataset.show = 'true'
    setActive((prev) => {
      const next = computeActive(view.state)
      return sameActive(prev, next) ? prev : next
    })
    const { from, to } = view.state.selection
    let start
    let end
    try {
      start = view.coordsAtPos(from)
      end = view.coordsAtPos(to)
    } catch {
      return
    }
    const centerLeft = (start.left + end.left) / 2
    el.style.left = `${centerLeft}px`
    el.style.top = `${start.top}px`
    // Re-measure after paint to sit fully above the selection, flip below when
    // there's no room, and clamp horizontally inside the viewport.
    const rafId = requestAnimationFrame(() => {
      const r = el.getBoundingClientRect()
      const vw = window.innerWidth
      let top = start.top - r.height - 8
      if (top < 4) top = end.bottom + 8
      let left = centerLeft - r.width / 2
      left = Math.max(4, Math.min(left, vw - r.width - 4))
      el.style.left = `${left}px`
      el.style.top = `${top}px`
    })
    return () => cancelAnimationFrame(rafId)
  })

  // Focus the URL field when link mode opens.
  useEffect(() => {
    if (linkMode) inputRef.current?.focus()
  }, [linkMode])

  function runAction(fn: (ctx: Ctx) => void) {
    if (loading) return
    getEditor()?.action((ctx) => fn(ctx))
  }

  function toggleMark(key: CmdKey<unknown>) {
    // onMouseDown already prevented default, so the editor keeps focus and the
    // selection survives — the command applies to the live selection.
    runAction((ctx) => ctx.get(commandsCtx).call(key))
  }

  function openLinkMode() {
    runAction((ctx) => setLinkValue(currentLinkHref(ctx.get(editorViewCtx).state)))
    setLinkMode(true)
  }

  function applyLink() {
    const href = linkValue.trim()
    runAction((ctx) => {
      const commands = ctx.get(commandsCtx)
      if (!href) {
        if (active.link) commands.call(toggleLinkCommand.key) // remove
      } else if (active.link) {
        commands.call(updateLinkCommand.key, { href })
      } else {
        commands.call(toggleLinkCommand.key, { href })
      }
      ctx.get(editorViewCtx).focus()
    })
    setLinkMode(false)
    setLinkValue('')
  }

  function cancelLink() {
    setLinkMode(false)
    setLinkValue('')
    runAction((ctx) => ctx.get(editorViewCtx).focus())
  }

  return (
    <div
      ref={ref}
      role="toolbar"
      aria-label="Format selection"
      className="tela-bubble-toolbar"
    >
      {linkMode ? (
        <Input
          ref={inputRef}
          size="sm"
          type="url"
          placeholder="Paste or type a link, then Enter"
          aria-label="Link URL"
          className="tela-bubble-link-input"
          value={linkValue}
          onChange={(e) => setLinkValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              applyLink()
            } else if (e.key === 'Escape') {
              e.preventDefault()
              cancelLink()
            }
          }}
          onBlur={cancelLink}
        />
      ) : (
        <>
          <BubbleButton
            label="Bold"
            active={active.strong}
            onClick={() => toggleMark(toggleStrongCommand.key)}
          >
            <Bold size="1em" strokeWidth={2.5} aria-hidden />
          </BubbleButton>
          <BubbleButton
            label="Italic"
            active={active.emphasis}
            onClick={() => toggleMark(toggleEmphasisCommand.key)}
          >
            <Italic size="1em" strokeWidth={2.5} aria-hidden />
          </BubbleButton>
          <BubbleButton
            label="Strikethrough"
            active={active.strike}
            onClick={() => toggleMark(toggleStrikethroughCommand.key)}
          >
            <Strikethrough size="1em" strokeWidth={2.5} aria-hidden />
          </BubbleButton>
          <BubbleButton
            label="Inline code"
            active={active.inlineCode}
            onClick={() => toggleMark(toggleInlineCodeCommand.key)}
          >
            <Code size="1em" strokeWidth={2.5} aria-hidden />
          </BubbleButton>
          <BubbleButton
            label="Highlight"
            active={active.highlight}
            onClick={() => toggleMark(toggleHighlightCommand.key)}
          >
            <Highlighter size="1em" strokeWidth={2.5} aria-hidden />
          </BubbleButton>
          <BubbleButton label="Link" active={active.link} onClick={openLinkMode}>
            <LinkIcon size="1em" strokeWidth={2.5} aria-hidden />
          </BubbleButton>
        </>
      )}
    </div>
  )
}

interface BubbleButtonProps {
  label: string
  active: boolean
  onClick: () => void
  children: React.ReactNode
}

function BubbleButton({ label, active, onClick, children }: BubbleButtonProps) {
  return (
    <button
      type="button"
      className={cn('tela-bubble-btn')}
      aria-label={label}
      aria-pressed={active}
      data-active={active ? 'true' : 'false'}
      // preventDefault keeps the editor focused + selection intact through the
      // click, so the toggled mark lands on the selected range.
      onMouseDown={(e) => {
        e.preventDefault()
        onClick()
      }}
    >
      {children}
    </button>
  )
}
