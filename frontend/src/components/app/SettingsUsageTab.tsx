import { useAdminUsage } from '../../lib/queries/admin-usage'
import { formatBytes } from '../../lib/format'
import type { AdminAccountUsage, AdminUsage, KnowledgeGap } from '../../lib/types'
import { Badge } from '../ui/badge'
import { cn } from '../../lib/utils'

// Instance-admin global usage overview: instance-wide totals, the top AI consumers
// this month, and the questions the docs keep failing to answer.
export function SettingsUsageTab() {
  const q = useAdminUsage()

  if (q.isLoading) {
    return <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">Loading usage…</p>
  }
  if (q.isError || !q.data) {
    return (
      <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
        Couldn't load usage.
      </p>
    )
  }
  const d: AdminUsage = q.data
  const answerRate = d.totals.asks > 0 ? Math.round((d.totals.asks_answered / d.totals.asks) * 100) : null

  return (
    <section aria-labelledby="settings-usage" className="flex flex-col gap-[var(--space-5)]">
      <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
        Instance-wide usage. Monthly figures are for{' '}
        <span className="text-[var(--text-primary)]">{d.period}</span> (UTC).
      </p>

      <div className="grid grid-cols-2 gap-[var(--space-3)] sm:grid-cols-4">
        <Stat label="Users" value={d.totals.users} />
        <Stat label="Orgs" value={d.totals.orgs} />
        <Stat label="Spaces" value={d.totals.spaces} />
        <Stat label="Pages" value={d.totals.pages} />
        <Stat label="Storage" value={formatBytes(d.totals.storage_bytes)} />
        <Stat label="AI calls / mo" value={d.totals.llm_calls} />
        <Stat label="Asks / mo" value={d.totals.asks} />
        <Stat label="Answer rate" value={answerRate == null ? '—' : `${answerRate}%`} />
      </div>

      <div className="flex flex-col gap-[var(--space-3)]">
        <h3 className="m-0 text-[length:var(--text-sm)] font-semibold text-[var(--text-primary)]">
          Top AI consumers this month
        </h3>
        {d.top.length > 0 ? (
          <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
            {d.top.map((a) => (
              <ConsumerRow key={`${a.kind}-${a.id}`} a={a} />
            ))}
          </ul>
        ) : (
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">No AI usage this month.</p>
        )}
      </div>

      <div className="flex flex-col gap-[var(--space-3)]">
        <h3 className="m-0 text-[length:var(--text-sm)] font-semibold text-[var(--text-primary)]">
          Top unanswered questions (30d)
        </h3>
        {d.gaps.length > 0 ? (
          <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
            {d.gaps.map((g, i) => (
              <GapRow key={i} g={g} />
            ))}
          </ul>
        ) : (
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
            Nothing logged — Ask analytics need the embedder + some questions.
          </p>
        )}
      </div>
    </section>
  )
}

function Stat({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="flex flex-col gap-[2px] rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--surface-1)] px-[var(--space-3)] py-[var(--space-2)]">
      <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
        {label}
      </span>
      <span className="text-[length:var(--text-lg)] font-semibold tabular-nums text-[var(--text-primary)]">
        {value}
      </span>
    </div>
  )
}

const rowCn = cn(
  'm-0 list-none flex items-center gap-[var(--space-3)]',
  'px-[var(--space-3)] py-[var(--space-2)]',
  'rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--surface-1)]',
)

function ConsumerRow({ a }: { a: AdminAccountUsage }) {
  const cap = a.llm_cap == null ? '∞' : a.llm_cap
  const over = a.llm_cap != null && a.llm_calls >= a.llm_cap
  return (
    <li className={rowCn}>
      <Badge variant="muted">{a.kind}</Badge>
      <span className="flex-1 min-w-0 truncate text-[length:var(--text-sm)] text-[var(--text-primary)]">
        {a.label}
        {a.plan_name ? (
          <span className="ml-[var(--space-2)] text-[length:var(--text-xs)] text-[var(--text-muted)]">{a.plan_name}</span>
        ) : null}
      </span>
      <span
        className={cn(
          'text-[length:var(--text-sm)] tabular-nums',
          over ? 'text-[var(--danger)] font-medium' : 'text-[var(--text-primary)]',
        )}
      >
        {a.llm_calls} / {cap}
      </span>
    </li>
  )
}

function GapRow({ g }: { g: KnowledgeGap }) {
  return (
    <li className={rowCn}>
      <span className="flex-1 min-w-0 truncate text-[length:var(--text-sm)] text-[var(--text-primary)]">
        {g.question}
      </span>
      <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] tabular-nums">
        asked {g.asks}× · {g.answered}/{g.asks} answered
      </span>
    </li>
  )
}
