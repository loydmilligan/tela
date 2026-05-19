import { useState } from 'react'
import { Pencil, Trash2, X } from 'lucide-react'
import type { Comment } from '../../lib/comments/use-comments'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import { ApiError } from '../../lib/api'
import { Button } from '../ui/button'
import { TextArea } from '../ui/textarea'
import { cn } from '../../lib/utils'

interface CommentItemProps {
  comment: Comment
  // Current viewer's user id — used to decide whether to surface own-author
  // edit/delete affordances.
  currentUserId: number
  // True iff the viewer's space role is 'owner'. Owners can delete any
  // comment in the page (matches M8 backend rule).
  isSpaceOwner: boolean
  onEdit: (id: number, body: string) => Promise<void>
  onDelete: (id: number) => Promise<void>
  // Optimistic rows have negative ids and a 'sending…' pulse.
  isOptimistic: boolean
  // Visual mute for resolved threads (#74). Drops opacity on the whole
  // item; pair with `strikethroughBody` to also draw a line through the
  // body text (root of a resolved thread only — replies stay readable).
  muted?: boolean
  // M8.5 — draws a light line-through across the body paragraph (mode
  // 'view' only). The CommentThread passes this true on the root of a
  // resolved thread so the body reads as "this comment has been settled"
  // without obscuring the words.
  strikethroughBody?: boolean
}

export function CommentItem({
  comment,
  currentUserId,
  isSpaceOwner,
  onEdit,
  onDelete,
  isOptimistic,
  muted = false,
  strikethroughBody = false,
}: CommentItemProps) {
  const [mode, setMode] = useState<'view' | 'edit'>('view')
  const [draft, setDraft] = useState(comment.body)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const isAuthor = comment.author_id === currentUserId
  const canEdit = isAuthor && !isOptimistic
  const canDelete = (isAuthor || isSpaceOwner) && !isOptimistic

  async function handleSave() {
    const trimmed = draft.trim()
    if (trimmed === comment.body) {
      setMode('view')
      return
    }
    if (trimmed.length === 0) {
      setError('Body is required.')
      return
    }
    setBusy(true)
    setError(null)
    try {
      await onEdit(comment.id, trimmed)
      setMode('view')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save.')
    } finally {
      setBusy(false)
    }
  }

  async function handleDeleteConfirmed() {
    setBusy(true)
    setError(null)
    try {
      await onDelete(comment.id)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete.')
      setBusy(false)
      setConfirmDelete(false)
    }
  }

  return (
    <div
      className={cn(
        'flex flex-col gap-[var(--space-1)] py-[var(--space-2)]',
        muted && 'opacity-60',
        isOptimistic && 'animate-pulse',
      )}
    >
      <div className="flex items-baseline gap-[var(--space-2)]">
        <span className="text-[length:var(--text-sm)] font-medium text-[var(--text-primary)] font-[family-name:var(--font-sans)]">
          {comment.author_username}
        </span>
        <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
          {isOptimistic ? 'sending…' : relativeTimeFromSqlite(comment.created_at)}
        </span>
        <div className="ml-auto flex items-center gap-[var(--space-1)]">
          {canEdit && mode === 'view' ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label="Edit comment"
              onClick={() => {
                setDraft(comment.body)
                setError(null)
                setMode('edit')
              }}
              className="h-[var(--space-6)] w-[var(--space-6)] p-0"
            >
              <Pencil width={12} height={12} />
            </Button>
          ) : null}
          {canDelete && mode === 'view' ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label="Delete comment"
              onClick={() => setConfirmDelete(true)}
              className="h-[var(--space-6)] w-[var(--space-6)] p-0 hover:text-[var(--danger)]"
            >
              <Trash2 width={12} height={12} />
            </Button>
          ) : null}
        </div>
      </div>

      {mode === 'edit' ? (
        <div className="flex flex-col gap-[var(--space-2)]">
          <TextArea
            font="sans"
            size="sm"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            disabled={busy}
            aria-label="Edit comment body"
          />
          {error ? (
            <p
              role="alert"
              className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]"
            >
              {error}
            </p>
          ) : null}
          <div className="flex justify-end gap-[var(--space-2)]">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                setMode('view')
                setError(null)
                setDraft(comment.body)
              }}
              disabled={busy}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="primary"
              size="sm"
              onClick={() => void handleSave()}
              disabled={busy || draft.trim().length === 0}
            >
              {busy ? 'Saving…' : 'Save'}
            </Button>
          </div>
        </div>
      ) : (
        <p
          className={cn(
            'm-0 whitespace-pre-wrap',
            'text-[length:var(--text-sm)] leading-[var(--leading-normal)]',
            'text-[var(--text-primary)] font-[family-name:var(--font-sans)]',
            strikethroughBody && 'line-through',
          )}
          style={
            // Light strikethrough so resolved root body reads as settled
            // without obscuring its text — colour-mixed against currentColor
            // so it tracks whatever colour the muted text already has.
            strikethroughBody
              ? {
                  textDecorationColor:
                    'color-mix(in srgb, currentColor 30%, transparent)',
                }
              : undefined
          }
        >
          {comment.body}
        </p>
      )}

      {confirmDelete ? (
        <div className="mt-[var(--space-1)] flex items-center justify-between gap-[var(--space-2)] rounded-[var(--radius-sm)] bg-[var(--surface-2)] px-[var(--space-2)] py-[var(--space-2)]">
          <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
            Delete this comment?
          </span>
          <div className="flex items-center gap-[var(--space-1)]">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setConfirmDelete(false)}
              disabled={busy}
              aria-label="Cancel delete"
            >
              <X width={12} height={12} /> Cancel
            </Button>
            <Button
              type="button"
              variant="danger"
              size="sm"
              onClick={() => void handleDeleteConfirmed()}
              disabled={busy}
            >
              {busy ? 'Deleting…' : 'Delete'}
            </Button>
          </div>
        </div>
      ) : null}

      {error && mode !== 'edit' ? (
        <p
          role="alert"
          className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]"
        >
          {error}
        </p>
      ) : null}
    </div>
  )
}
