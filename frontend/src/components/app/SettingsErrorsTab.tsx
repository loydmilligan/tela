import { useState } from 'react'
import {
  useClientErrorGroups,
  useClientErrorOccurrences,
  type ClientErrorGroup,
} from '../../lib/queries/client-errors'
import { IncludeAdminsToggle } from './IncludeAdminsToggle'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import { Badge } from '../ui/badge'
import { cn } from '../../lib/utils'

// Instance-admin "Issues" view over browser error reports — client.error events
// grouped by fingerprint so a recurring crash is one row with a count, not a
// flood in the raw Events feed. Expand a row for its stack + recent occurrences.
export function SettingsErrorsTab() {
  const [includeAdmins, setIncludeAdmins] = useState(false)
  const groups = useClientErrorGroups(includeAdmins)

  return (
    <section aria-labelledby="settings-errors" className="flex flex-col gap-[var(--space-4)]">
      <div className="flex items-start justify-between gap-[var(--space-3)]">
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
          Errors reported from users' browsers — uncaught exceptions, failed
          requests, broken assets — grouped by signature. Most recently seen first.
        </p>
        <IncludeAdminsToggle checked={includeAdmins} onChange={setIncludeAdmins} />
      </div>

      {groups.isLoading ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">Loading errors…</p>
      ) : groups.isError ? (
        <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
          Couldn't load errors.
        </p>
      ) : groups.data && groups.data.length > 0 ? (
        <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-2)]">
          {groups.data.map((g) => (
            <ErrorGroupRow key={g.fingerprint} group={g} includeAdmins={includeAdmins} />
          ))}
        </ul>
      ) : (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          No client errors reported. 🎉
        </p>
      )}
    </section>
  )
}

function ErrorGroupRow({
  group,
  includeAdmins,
}: {
  group: ClientErrorGroup
  includeAdmins: boolean
}) {
  const [open, setOpen] = useState(false)
  const occ = useClientErrorOccurrences(open ? group.fingerprint : null, includeAdmins)

  return (
    <li
      className={cn(
        'm-0 list-none flex flex-col',
        'rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--surface-1)]',
      )}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex items-center gap-[var(--space-3)] px-[var(--space-3)] py-[var(--space-3)] text-left"
      >
        <Badge variant="muted" className="shrink-0">
          {group.kind}
        </Badge>
        <span className="flex-1 min-w-0 truncate text-[length:var(--text-sm)] font-medium text-[var(--text-primary)] font-[family-name:var(--font-sans)]">
          {group.message}
        </span>
        <span className="shrink-0 text-[length:var(--text-xs)] text-[var(--text-muted)]">
          {group.count}× · {group.users} user{group.users === 1 ? '' : 's'} ·{' '}
          {relativeTimeFromSqlite(group.last_seen)}
        </span>
      </button>

      {open ? (
        <div className="flex flex-col gap-[var(--space-3)] border-t border-[var(--border-subtle)] px-[var(--space-3)] py-[var(--space-3)]">
          <pre className="m-0 max-h-[14rem] overflow-auto whitespace-pre-wrap break-words rounded-[var(--radius-sm)] bg-[var(--surface-2)] p-[var(--space-2)] text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-mono)]">
            {group.sample}
          </pre>
          <div className="flex flex-col gap-[var(--space-1)]">
            <span className="text-[length:var(--text-xs)] font-medium uppercase tracking-wide text-[var(--text-muted)]">
              Recent occurrences
            </span>
            {occ.isLoading ? (
              <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">Loading…</span>
            ) : occ.data && occ.data.length > 0 ? (
              <ul className="m-0 p-0 list-none flex flex-col gap-[1px]">
                {occ.data.map((o) => (
                  <li
                    key={o.id}
                    className="flex items-baseline gap-[var(--space-2)] text-[length:var(--text-xs)] text-[var(--text-muted)]"
                  >
                    <span className="font-medium text-[var(--text-primary)]">
                      {o.actor_label || 'anonymous'}
                    </span>
                    <span>{relativeTimeFromSqlite(o.created_at)}</span>
                    {o.ip ? <span>· {o.ip}</span> : null}
                  </li>
                ))}
              </ul>
            ) : (
              <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">None.</span>
            )}
          </div>
        </div>
      ) : null}
    </li>
  )
}
