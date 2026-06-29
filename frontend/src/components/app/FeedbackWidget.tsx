import { useCallback, useEffect, useRef, useState } from 'react'
import { Check, MessageSquarePlus } from 'lucide-react'
import { Button } from '../ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover'
import { TextArea } from '../ui/textarea'
import { cn } from '../../lib/utils'
import { useCreateFeedback } from '../../lib/queries/feedback'
import { collectFeedbackContext } from '../../lib/feedbackContext'
import { subscribeToOpenFeedback } from '../../lib/feedbackEvent'
import { router } from '../../routes/router'
import type { FeedbackKind } from '../../lib/types'

const KINDS: { value: FeedbackKind; label: string }[] = [
  { value: 'idea', label: 'Idea' },
  { value: 'bug', label: 'Bug' },
  { value: 'other', label: 'Other' },
]

const BODY_MAX = 8000

// Read the live route the same way AppCommandHost does — off the imported
// router instance, so this works whether the popover was opened from its own
// header trigger or programmatically from the user menu / command palette.
function readRoute(): { pathname: string; spaceId: number | null; pageId: number | null } {
  let spaceId: number | null = null
  let pageId: number | null = null
  for (const m of router.state.matches) {
    const p = m.params as { spaceId?: number; pageId?: number }
    if (typeof p.spaceId === 'number') spaceId = p.spaceId
    if (typeof p.pageId === 'number') pageId = p.pageId
  }
  return { pathname: router.state.location.pathname, spaceId, pageId }
}

// In-app feedback: a quiet header trigger that blooms a small popover (NOT a
// blocking modal) with one textarea and optional type chips. Email + provenance
// are taken from the session and the route silently — no identity or context is
// ever asked for. The same backend core powers the MCP submit_feedback tool, so
// human and agent reports land in one inbox. Mounted once in the app header; the
// user-menu item and ⌘K command open this instance via the feedback event bus.
export function FeedbackWidget() {
  const [open, setOpen] = useState(false)
  const [kind, setKind] = useState<FeedbackKind | null>(null)
  const [text, setText] = useState('')
  const [status, setStatus] = useState<'idle' | 'sending' | 'sent' | 'error'>('idle')
  const textRef = useRef<HTMLTextAreaElement>(null)
  const create = useCreateFeedback()

  // User-menu item + command palette open the single mounted instance.
  useEffect(() => subscribeToOpenFeedback(() => setOpen(true)), [])

  // After a send lands, the close itself is the acknowledgment (Geist): hold the
  // gentle "thanks" briefly, then dismiss — no toast, no celebratory modal.
  useEffect(() => {
    if (status !== 'sent') return
    const t = setTimeout(() => setOpen(false), 1500)
    return () => clearTimeout(t)
  }, [status])

  function reset() {
    setKind(null)
    setText('')
    setStatus('idle')
  }

  function handleOpenChange(next: boolean) {
    // Never let an outside click / Esc discard an in-flight send; otherwise
    // close freely. Reset only when closing (preserve the draft on error so a
    // failed send doesn't lose what was typed).
    if (!next && status === 'sending') return
    setOpen(next)
    if (!next) reset()
  }

  const submit = useCallback(async () => {
    const body = text.trim()
    if (!body || status === 'sending') return
    setStatus('sending')
    try {
      await create.mutateAsync({
        body,
        kind: kind ?? undefined,
        context: collectFeedbackContext(readRoute()),
      })
      setStatus('sent')
    } catch {
      setStatus('error')
    }
  }, [text, kind, status, create])

  const canSend = text.trim().length > 0 && status !== 'sending'
  const remaining = BODY_MAX - text.length

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          aria-label="Send feedback"
          className="h-[var(--space-8)] w-[var(--space-8)] p-0"
        >
          <MessageSquarePlus width={16} height={16} aria-hidden />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        sideOffset={8}
        className="w-[21rem] p-[var(--space-4)]"
        // Focus the textarea on open (not the first chip), so typing is immediate.
        onOpenAutoFocus={(e) => {
          if (status === 'sent') return
          e.preventDefault()
          textRef.current?.focus()
        }}
      >
        {status === 'sent' ? (
          <div className="flex flex-col items-center gap-[var(--space-2)] py-[var(--space-4)] text-center">
            <span className="flex items-center justify-center h-[var(--space-7)] w-[var(--space-7)] rounded-full bg-[color-mix(in_srgb,var(--success)_15%,transparent)] text-[var(--success)]">
              <Check width={18} height={18} aria-hidden />
            </span>
            <p className="m-0 text-[length:var(--text-sm)] font-medium text-[var(--text-primary)]">
              Thanks — we read every message.
            </p>
          </div>
        ) : (
          <form
            onSubmit={(e) => {
              e.preventDefault()
              void submit()
            }}
            className="flex flex-col gap-[var(--space-3)]"
          >
            <div className="flex items-center justify-between">
              <p className="m-0 text-[length:var(--text-sm)] font-semibold text-[var(--text-primary)]">
                Send feedback
              </p>
              <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">
                to the tela team
              </span>
            </div>

            <div role="group" aria-label="Type" className="flex gap-[var(--space-2)]">
              {KINDS.map((k) => {
                const selected = kind === k.value
                return (
                  <button
                    key={k.value}
                    type="button"
                    aria-pressed={selected}
                    onClick={() => setKind(selected ? null : k.value)}
                    className={cn(
                      'px-[var(--space-3)] py-[var(--space-1)] rounded-[var(--radius-sm)]',
                      'text-[length:var(--text-xs)] border transition-colors duration-[var(--duration-fast)]',
                      'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]',
                      selected
                        ? 'border-[var(--accent)] text-[var(--accent)] bg-[color-mix(in_srgb,var(--accent)_12%,transparent)]'
                        : 'border-[var(--border-subtle)] text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:border-[var(--border-strong)]',
                    )}
                  >
                    {k.label}
                  </button>
                )
              })}
            </div>

            <TextArea
              ref={textRef}
              font="sans"
              size="sm"
              value={text}
              onChange={(e) => {
                setText(e.target.value)
                if (status === 'error') setStatus('idle')
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault()
                  void submit()
                }
              }}
              maxLength={BODY_MAX}
              rows={4}
              placeholder="Share an idea, a bug, or anything on your mind…"
              aria-label="Your feedback"
              className="resize-none"
            />

            {status === 'error' ? (
              <p role="alert" className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]">
                Couldn't send — please try again.
              </p>
            ) : null}

            <div className="flex items-center justify-between">
              <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">
                {remaining < 500
                  ? `${remaining} left`
                  : '⌘↵ to send'}
              </span>
              <Button type="submit" size="sm" disabled={!canSend}>
                {status === 'sending' ? 'Sending…' : 'Send'}
              </Button>
            </div>
          </form>
        )}
      </PopoverContent>
    </Popover>
  )
}
