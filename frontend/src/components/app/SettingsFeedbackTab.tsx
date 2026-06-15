import { useAdminFeedback } from '../../lib/queries/admin-usage'
import { localDateFromSqlite } from '../../lib/relativeTime'
import type { FeedbackEntry } from '../../lib/types'
import { Badge } from '../ui/badge'
import { cn } from '../../lib/utils'

// Instance-admin inbox for feedback submitted via the in-app form or the MCP
// submit_feedback tool. Read-only, newest first.
export function SettingsFeedbackTab() {
  const fb = useAdminFeedback()

  return (
    <section aria-labelledby="settings-feedback" className="flex flex-col gap-[var(--space-4)]">
      <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
        Feedback your users sent about tela — from the in-app form or an agent's
        submit_feedback tool. Most recent first.
      </p>

      {fb.isLoading ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">Loading feedback…</p>
      ) : fb.isError ? (
        <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
          Couldn't load feedback.
        </p>
      ) : fb.data && fb.data.length > 0 ? (
        <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-2)]">
          {fb.data.map((e) => (
            <FeedbackRow key={e.id} entry={e} />
          ))}
        </ul>
      ) : (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">No feedback yet.</p>
      )}
    </section>
  )
}

function FeedbackRow({ entry }: { entry: FeedbackEntry }) {
  const who = entry.username ?? (entry.user_id ? `user #${entry.user_id}` : 'unknown')
  return (
    <li
      className={cn(
        'm-0 list-none flex flex-col gap-[var(--space-2)]',
        'px-[var(--space-3)] py-[var(--space-3)]',
        'rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--surface-1)]',
      )}
    >
      <div className="flex items-start justify-between gap-[var(--space-3)]">
        <span className="text-[length:var(--text-sm)] font-medium text-[var(--text-primary)] font-[family-name:var(--font-sans)]">
          {entry.subject}
        </span>
        {entry.via_api_key ? <Badge variant="muted">via agent</Badge> : null}
      </div>
      <p className="m-0 whitespace-pre-wrap text-[length:var(--text-sm)] text-[var(--text-primary)] leading-[var(--leading-relaxed)]">
        {entry.body}
      </p>
      <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
        {who} · {localDateFromSqlite(entry.created_at)}
      </span>
    </li>
  )
}
