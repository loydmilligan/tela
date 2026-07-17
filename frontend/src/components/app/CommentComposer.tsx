import { useState } from 'react'
import type { CommentAnchor } from '../../lib/comments/anchor'
import { ApiError } from '../../lib/api'
import { Button } from '../ui/button'
import { Checkbox } from '../ui/checkbox'
import { Input } from '../ui/input'
import { Select } from '../ui/select'
import { TextArea } from '../ui/textarea'
import { cn } from '../../lib/utils'

// Change-comment vocabulary. Mirrors the backend convention in
// docs/page-properties.md; `change` is what the per-page changelog filters on,
// and what autoChangeComment writes on save.
const CHANGE_TYPES = ['change', 'decision', 'fix', 'note', 'deprecation']
const CHANGE_STATUSES = ['', 'open', 'done', 'superseded']

interface CommentComposerProps {
  // True when the editor currently has a non-empty selection. When false,
  // the textarea + submit are disabled and a hint is shown instead.
  hasSelection: boolean
  // Snapshot the current editor selection at submit time. Returns null if the
  // selection has collapsed between hint-render and submit (race window).
  captureAnchor: () => CommentAnchor | null
  onSubmit: (input: {
    body: string
    anchor_prefix: string
    anchor_exact: string
    anchor_suffix: string
    props?: Record<string, unknown>
  }) => Promise<void>
  // When non-null, surfaces the exact text that would be anchored on
  // submit. Drives the inline "Commenting on: …" preview above the
  // textarea so the user can confirm the right passage is selected.
  anchorPreview: string | null
}

export function CommentComposer({
  hasSelection,
  captureAnchor,
  onSubmit,
  anchorPreview,
}: CommentComposerProps) {
  const [body, setBody] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Change-comment mode: OFF by default, so the common case (a plain remark) is
  // untouched. On, it attaches the structured props that make the comment
  // queryable via `query{ target: comments }`.
  const [isChange, setIsChange] = useState(false)
  const [changeSummary, setChangeSummary] = useState('')
  const [changeType, setChangeType] = useState('change')
  const [changeStatus, setChangeStatus] = useState('')

  const disabled = !hasSelection || busy

  async function handleSubmit() {
    const trimmed = body.trim()
    if (!trimmed) {
      setError('Body is required.')
      return
    }
    const anchor = captureAnchor()
    if (!anchor) {
      setError('Select a passage in the editor before commenting.')
      return
    }
    // change_summary, NOT summary: `summary` is the page's own abstract (what
    // the page is about, written by the auto-summarizer); this is what CHANGED.
    // Different lanes — sharing the key would invite conflating them.
    let props: Record<string, unknown> | undefined
    if (isChange) {
      const s = changeSummary.trim()
      if (!s) {
        setError('A change comment needs a summary.')
        return
      }
      props = { type: changeType, change_summary: s }
      if (changeStatus) props.status = changeStatus
    }
    setBusy(true)
    setError(null)
    try {
      await onSubmit({
        body: trimmed,
        anchor_prefix: anchor.prefix,
        anchor_exact: anchor.exact,
        anchor_suffix: anchor.suffix,
        props,
      })
      setBody('')
      setChangeSummary('')
      setIsChange(false)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to post comment.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-col gap-[var(--space-2)]">
      {hasSelection && anchorPreview ? (
        <div
          className={cn(
            'rounded-[var(--radius-sm)] px-[var(--space-3)] py-[var(--space-2)]',
            'bg-[var(--surface-2)] border-l-2 border-[var(--accent)]',
            'text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]',
            'overflow-hidden',
          )}
        >
          <div className="mb-[2px] uppercase tracking-wider">
            Commenting on
          </div>
          <div
            className={cn(
              'truncate text-[var(--text-primary)]',
              'whitespace-pre-wrap line-clamp-3',
            )}
          >
            {anchorPreview}
          </div>
        </div>
      ) : null}
      {!hasSelection ? (
        <p
          className={cn(
            'm-0 px-[var(--space-3)] py-[var(--space-2)]',
            'rounded-[var(--radius-sm)] bg-[var(--surface-2)]',
            'text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]',
          )}
        >
          Select text in the editor to comment on a passage.
        </p>
      ) : null}
      <TextArea
        font="sans"
        size="sm"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        placeholder={hasSelection ? 'Add a comment…' : 'Select a passage first'}
        disabled={disabled}
        aria-label="New comment body"
      />
      {isChange ? (
        <div className="flex flex-col gap-[var(--space-2)]">
          <Input
            size="sm"
            value={changeSummary}
            onChange={(e) => setChangeSummary(e.target.value)}
            placeholder="What changed? (short summary)"
            disabled={disabled}
            aria-label="Change summary"
          />
          <div className="flex gap-[var(--space-2)]">
            <Select
              size="sm"
              value={changeType}
              onChange={(e) => setChangeType(e.target.value)}
              disabled={disabled}
              aria-label="Change type"
              className="flex-1"
            >
              {CHANGE_TYPES.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </Select>
            <Select
              size="sm"
              value={changeStatus}
              onChange={(e) => setChangeStatus(e.target.value)}
              disabled={disabled}
              aria-label="Change status"
              className="flex-1"
            >
              {CHANGE_STATUSES.map((st) => (
                <option key={st || 'none'} value={st}>
                  {st || 'no status'}
                </option>
              ))}
            </Select>
          </div>
        </div>
      ) : null}
      {error ? (
        <p
          role="alert"
          className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]"
        >
          {error}
        </p>
      ) : null}
      <div className="flex items-center justify-between gap-[var(--space-2)]">
        <label
          className={cn(
            'flex items-center gap-[var(--space-2)] cursor-pointer select-none',
            'text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]',
          )}
        >
          <Checkbox
            checked={isChange}
            onCheckedChange={(v) => setIsChange(v === true)}
            disabled={disabled}
            aria-label="Log this comment as a change"
          />
          Log as change
        </label>
        <Button
          type="button"
          variant="primary"
          size="sm"
          onClick={() => void handleSubmit()}
          disabled={disabled || body.trim().length === 0}
        >
          {busy ? 'Posting…' : isChange ? 'Log change' : 'Comment'}
        </Button>
      </div>
    </div>
  )
}
