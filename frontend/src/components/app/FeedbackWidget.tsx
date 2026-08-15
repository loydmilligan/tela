import { useCallback, useEffect, useRef, useState } from 'react'
import { Check, MessageSquarePlus } from 'lucide-react'
import { Button } from '../ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover'
import { TextArea } from '../ui/textarea'
import { Input } from '../ui/input'
import { cn } from '../../lib/utils'
import { useRouterState } from '@tanstack/react-router'
import {
  useCreateFeedback,
  useFeedbackForPage,
  useFeedbackOptions,
} from '../../lib/queries/feedback'
import { Checkbox } from '../ui/checkbox'
import { Select } from '../ui/select'
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
  // Recipient: '' = instance default; 'u:<id>' / 'g:<id>' otherwise.
  const [target, setTarget] = useState('')
  const [askClaude, setAskClaude] = useState(false)
  // kind=other requires an explicit title + note type (2b routing).
  const [otherTitle, setOtherTitle] = useState('')
  const [otherType, setOtherType] = useState('note')
  const options = useFeedbackOptions(open)
  // Re-render on navigation so the has-issue badge tracks the current page.
  useRouterState({ select: (s) => s.location.pathname })
  const routeNow = readRoute()
  const forPage = useFeedbackForPage(routeNow.pageId)
  const hasIssue = (forPage.data?.count ?? 0) > 0
  const [status, setStatus] = useState<'idle' | 'sending' | 'sent' | 'error'>('idle')
  const textRef = useRef<HTMLTextAreaElement>(null)
  // True for a short window after a programmatic open (user-menu / cmdk). Those
  // triggers live in a Radix dropdown / cmdk dialog that, as it closes, restores
  // focus to its OWN trigger — a focus-outside event that would dismiss this
  // popover the same frame it opens. We swallow exactly those dismissals.
  const suppressDismissRef = useRef(false)
  const create = useCreateFeedback()

  // User-menu item + command palette open the single mounted instance.
  useEffect(
    () =>
      subscribeToOpenFeedback(() => {
        suppressDismissRef.current = true
        setOpen(true)
        setTimeout(() => {
          suppressDismissRef.current = false
        }, 300)
      }),
    [],
  )

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
    setTarget('')
    setAskClaude(false)
    setOtherTitle('')
    setOtherType('note')
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
    if (kind === 'other' && !otherTitle.trim()) return
    setStatus('sending')
    try {
      const context = collectFeedbackContext(readRoute())
      if (kind === 'other') context.note_type = otherType
      await create.mutateAsync({
        body,
        subject: kind === 'other' ? otherTitle.trim() : undefined,
        kind: kind ?? undefined,
        context,
        recipient_user_id: target.startsWith('u:') ? Number(target.slice(2)) : undefined,
        recipient_group_id: target.startsWith('g:') ? Number(target.slice(2)) : undefined,
        claude_requested: askClaude || undefined,
      })
      setStatus('sent')
    } catch {
      setStatus('error')
    }
  }, [text, kind, status, create, target, askClaude, otherTitle, otherType])

  const canSend =
    text.trim().length > 0 &&
    status !== 'sending' &&
    (kind !== 'other' || otherTitle.trim().length > 0)
  const remaining = BODY_MAX - text.length

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          aria-label={hasIssue ? 'Send feedback (this note has an issue logged)' : 'Send feedback'}
          title={hasIssue ? 'This note already has an issue logged' : undefined}
          className="relative h-[var(--space-8)] w-[var(--space-8)] p-0"
        >
          <MessageSquarePlus width={16} height={16} aria-hidden />
          {hasIssue ? (
            <span
              aria-hidden
              className="absolute top-[var(--space-1)] right-[var(--space-1)] h-[var(--space-2)] w-[var(--space-2)] rounded-full bg-[var(--warning)]"
            />
          ) : null}
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        sideOffset={8}
        className="w-[21rem] p-[var(--space-4)]"
        // Swallow the dropdown/cmdk focus-restore dismiss right after a
        // programmatic open (see suppressDismissRef); normal outside clicks
        // still close the popover.
        onInteractOutside={(e) => {
          if (suppressDismissRef.current) e.preventDefault()
        }}
        // Focus the textarea on open (not the first chip), so typing is immediate.
        onOpenAutoFocus={(e) => {
          if (status === 'sent') return
          e.preventDefault()
          textRef.current?.focus()
        }}
      >
        {options.data && !options.data.enabled ? (
          <p className="m-0 py-[var(--space-3)] text-[length:var(--text-sm)] text-[var(--text-muted)] text-center">
            Feedback is turned off for your account.
          </p>
        ) : status === 'sent' ? (
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
                stays on this tela
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

            {kind === 'other' ? (
              <div className="flex flex-col gap-[var(--space-2)]">
                <Input
                  size="sm"
                  value={otherTitle}
                  onChange={(e) => setOtherTitle(e.target.value)}
                  placeholder="Title (required)"
                  aria-label="Title"
                />
                <Select
                  aria-label="Note type"
                  size="sm"
                  value={otherType}
                  onChange={(e) => setOtherType(e.target.value)}
                >
                  <option value="note">Note</option>
                  <option value="question">Question</option>
                  <option value="request">Request</option>
                </Select>
              </div>
            ) : null}

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

            <Select
              aria-label="Send to"
              size="sm"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
            >
              <option value="">
                {defaultTargetLabel(options.data) ?? 'Anyone (instance default)'}
              </option>
              {(options.data?.groups ?? []).map((g) => (
                <option key={`g${g.id}`} value={`g:${g.id}`}>
                  Group: {g.name}
                </option>
              ))}
              {(options.data?.users ?? []).map((u) => (
                <option key={`u${u.id}`} value={`u:${u.id}`}>
                  {u.username}
                </option>
              ))}
            </Select>

            {options.data?.allow_claude ? (
              <label className="inline-flex items-center gap-[var(--space-2)] text-[length:var(--text-xs)] text-[var(--text-muted)]">
                <Checkbox
                  checked={askClaude}
                  onCheckedChange={(v) => setAskClaude(v === true)}
                  aria-label="Request Claude triage"
                />
                Request Claude triage
              </label>
            ) : null}

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

// Label for the composer's default option: names the instance default target
// when one is configured, so "send" is never a mystery destination.
function defaultTargetLabel(
  opts?: import('../../lib/types').FeedbackOptions,
): string | null {
  if (!opts) return null
  if (opts.default.group_id != null) {
    const g = opts.groups.find((x) => x.id === opts.default.group_id)
    if (g) return `Group: ${g.name} (default)`
  }
  if (opts.default.user_id != null) {
    const u = opts.users.find((x) => x.id === opts.default.user_id)
    if (u) return `${u.username} (default)`
  }
  return null
}
