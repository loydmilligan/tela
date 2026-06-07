import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Download, FileText, Paperclip, Trash2, CornerLeftUp } from 'lucide-react'
import { cn } from '../../lib/utils'
import {
  attachmentKeys,
  useAttachments,
  useDeleteAttachment,
  type Attachment,
} from '../../lib/queries/attachments'

// The page Attachments strip — a compact chip row shown directly below the page
// title (reader + editor). Lists every file parented to the page (uploads AND
// files rclone-synced into its folder), so synced files are visible without any
// body edit. Images show as thumbnails, other files as icon chips. Embedded
// files (already placed inline in the body) carry a small marker so the manifest
// stays complete without being confusing. In editable mode each chip can be
// inserted into the body or deleted.

const COLLAPSED_LIMIT = 8

function isImageMime(mime: string): boolean {
  return mime.startsWith('image/')
}

function prettySize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const kb = bytes / 1024
  if (kb < 1024) return `${kb < 10 ? kb.toFixed(1) : Math.round(kb)} KB`
  const mb = kb / 1024
  return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`
}

export interface AttachmentStripViewProps {
  attachments: Attachment[]
  editable?: boolean
  /** Insert a file into the body at the cursor (editor only). */
  onInsert?: (a: Attachment) => void
  /** Remove a file from the page (editor only). */
  onDelete?: (a: Attachment) => void
  deletingId?: number | null
}

// Presentational strip — no data fetching, so it's directly Storybook-able.
export function AttachmentStripView({
  attachments,
  editable = false,
  onInsert,
  onDelete,
  deletingId,
}: AttachmentStripViewProps) {
  const [expanded, setExpanded] = useState(false)
  if (attachments.length === 0) return null

  const hidden = Math.max(0, attachments.length - COLLAPSED_LIMIT)
  const shown = expanded ? attachments : attachments.slice(0, COLLAPSED_LIMIT)

  return (
    <div
      className="flex flex-wrap items-center gap-[var(--space-2)] py-[var(--space-2)]"
      aria-label={`Attachments (${attachments.length})`}
    >
      <Paperclip
        aria-hidden
        width={14}
        height={14}
        className="text-[var(--text-muted)] shrink-0"
      />
      {shown.map((a) => (
        <AttachmentChip
          key={a.id}
          attachment={a}
          editable={editable}
          onInsert={onInsert}
          onDelete={onDelete}
          deleting={deletingId === a.id}
        />
      ))}
      {hidden > 0 && !expanded ? (
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className={cn(
            'text-[length:var(--text-sm)] text-[var(--text-muted)]',
            'rounded-[var(--radius-sm)] px-[var(--space-2)] py-[var(--space-1)]',
            'hover:text-[var(--text-primary)] hover:bg-[var(--surface-3)]',
          )}
        >
          +{hidden} more
        </button>
      ) : null}
    </div>
  )
}

interface AttachmentChipProps {
  attachment: Attachment
  editable: boolean
  onInsert?: (a: Attachment) => void
  onDelete?: (a: Attachment) => void
  deleting?: boolean
}

function AttachmentChip({
  attachment: a,
  editable,
  onInsert,
  onDelete,
  deleting,
}: AttachmentChipProps) {
  const image = isImageMime(a.mime)
  return (
    <span
      className={cn(
        'group inline-flex items-center gap-[var(--space-2)] max-w-[16rem]',
        'rounded-[var(--radius-sm)] border border-[var(--border-subtle)]',
        'bg-[var(--surface-2)] pl-[var(--space-1)] pr-[var(--space-2)] py-[var(--space-1)]',
        'text-[length:var(--text-sm)]',
        deleting && 'opacity-50',
      )}
    >
      <a
        href={a.url}
        target="_blank"
        rel="noreferrer"
        download={image ? undefined : a.name}
        className="inline-flex items-center gap-[var(--space-2)] min-w-0"
        title={a.name}
      >
        {image ? (
          <img
            src={a.url}
            alt=""
            className="h-5 w-5 rounded-[var(--radius-xs)] object-cover shrink-0"
          />
        ) : (
          <FileText
            aria-hidden
            width={16}
            height={16}
            className="text-[var(--text-muted)] shrink-0"
          />
        )}
        <span className="truncate text-[var(--text-primary)]">{a.name}</span>
        <span className="text-[var(--text-muted)] shrink-0">
          {prettySize(a.byte_size)}
        </span>
      </a>
      {a.embedded ? (
        <CornerLeftUp
          aria-label="Embedded in page"
          width={13}
          height={13}
          className="text-[var(--text-muted)] shrink-0"
        />
      ) : null}
      {editable ? (
        <span className="inline-flex items-center gap-[var(--space-1)] shrink-0">
          {onInsert && !a.embedded ? (
            <button
              type="button"
              onClick={() => onInsert(a)}
              aria-label={`Insert ${a.name} into the page`}
              className="text-[var(--text-muted)] hover:text-[var(--text-primary)]"
            >
              <Download width={13} height={13} className="rotate-180" aria-hidden />
            </button>
          ) : null}
          {onDelete ? (
            <button
              type="button"
              onClick={() => onDelete(a)}
              disabled={deleting}
              aria-label={`Remove ${a.name}`}
              className="text-[var(--text-muted)] hover:text-[var(--accent-negative-fg)]"
            >
              <Trash2 width={13} height={13} aria-hidden />
            </button>
          ) : null}
        </span>
      ) : null}
    </span>
  )
}

export interface AttachmentStripProps {
  pageId: number
  editable?: boolean
  onInsert?: (a: Attachment) => void
}

// Data container: fetches the page's attachments and wires delete. Renders
// nothing while empty/loading so it never adds blank chrome to a page.
export function AttachmentStrip({
  pageId,
  editable = false,
  onInsert,
}: AttachmentStripProps) {
  const { data } = useAttachments(pageId)
  const del = useDeleteAttachment(pageId)
  const qc = useQueryClient()
  // Refetch when the editor uploads a file (drop/paste) so it appears in the strip.
  useEffect(() => {
    const onChange = (e: Event) => {
      if ((e as CustomEvent<{ pageId: number }>).detail?.pageId === pageId) {
        void qc.invalidateQueries({ queryKey: attachmentKeys.page(pageId) })
      }
    }
    window.addEventListener('tela:attachments-changed', onChange)
    return () => window.removeEventListener('tela:attachments-changed', onChange)
  }, [pageId, qc])
  if (!data || data.length === 0) return null
  return (
    <AttachmentStripView
      attachments={data}
      editable={editable}
      onInsert={onInsert}
      onDelete={editable ? (a) => del.mutate(a.id) : undefined}
      deletingId={del.isPending ? (del.variables as number) : null}
    />
  )
}
