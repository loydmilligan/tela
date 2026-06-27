import { navigateToPage } from '../../lib/pageHitItem'
import { useAIUsage } from '../../lib/queries/ai-usage'
import {
  useAdminStats,
  type AdminStats,
  type StatsSignup,
  type StatsTopPage,
  type StatsTopPerson,
  type StatsTopSpace,
  type StatsUnanswered,
} from '../../lib/queries/admin-stats'
import { Sparkline } from '../ui/sparkline'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import { cn } from '../../lib/utils'

// Settings → Insights — the instance-analytics dashboard (instance-admin).
// Activity trends, growth, leaderboards, AI + error pulse, and knowledge health,
// all aggregated server-side from the events log. See backend admin_stats.go.
export function SettingsInsightsTab() {
  const q = useAdminStats()

  if (q.isLoading) {
    return <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">Loading insights…</p>
  }
  if (q.isError || !q.data) {
    return (
      <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
        Couldn't load insights.
      </p>
    )
  }
  const s = q.data
  const sum = (a: number[]) => a.reduce((x, y) => x + y, 0)

  return (
    <section aria-labelledby="settings-insights" className="flex flex-col gap-[var(--space-6)]">
      <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
        Activity across the instance over the last 30 days. Trends are daily; totals
        and leaderboards cover the window.
      </p>

      {/* Headline metrics */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-[var(--space-3)]">
        <Metric label="Active users · 7d" value={s.wau} sub={`${s.dau} today · ${s.mau} · 30d`} />
        <Metric label="Users" value={s.users} series={s.users_cum} tone="accent" />
        <Metric label="Pages" value={s.pages} series={s.pages_cum} tone="accent" />
        <Metric label="Spaces" value={s.spaces} />
      </div>

      {/* Signups & activation */}
      <Group title="Signups & activation">
        <div className="grid grid-cols-1 lg:grid-cols-[auto_1fr] gap-[var(--space-4)]">
          <div className="grid grid-cols-2 gap-[var(--space-3)] content-start">
            <Metric label="New users · 30d" value={s.new_users_30} tone="accent" />
            <Metric
              label="Activated"
              value={s.activated}
              sub={`of ${s.users} ever wrote a page`}
            />
          </div>
          <TopList title="Recent signups" empty="No signups yet.">
            {s.recent_signups.map((u) => (
              <SignupRow key={u.user_id} u={u} />
            ))}
          </TopList>
        </div>
      </Group>

      {/* Activity trends */}
      <Group title="Activity">
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-[var(--space-3)]">
          <Metric label="Page views" value={sum(s.views)} series={s.views} tone="accent" />
          <Metric label="Edits" value={sum(s.edits)} series={s.edits} tone="accent" />
          <Metric label="Sign-ins" value={sum(s.logins)} series={s.logins} />
          <Metric label="Asks" value={sum(s.asks)} series={s.asks} />
          <Metric
            label="Client errors"
            value={sum(s.errors)}
            series={s.errors}
            tone={sum(s.errors) > 0 ? 'danger' : 'muted'}
          />
        </div>
      </Group>

      {/* Leaderboards */}
      <Group title="Top content & people">
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-[var(--space-4)]">
          <TopList title="Most-viewed pages" empty="No views yet.">
            {s.top_pages.map((p) => (
              <PageRow key={p.page_id} p={p} />
            ))}
          </TopList>
          <TopList title="Top contributors" empty="No edits yet.">
            {s.top_contributors.map((c) => (
              <LabelRow key={c.label} label={c.label} count={c.count} />
            ))}
          </TopList>
          <TopList title="Most-active spaces" empty="No activity yet.">
            {s.top_spaces.map((sp) => (
              <SpaceRow key={sp.space_id} s={sp} />
            ))}
          </TopList>
        </div>
      </Group>

      {/* AI + errors + knowledge health */}
      <Group title="AI, errors & knowledge health">
        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-[var(--space-3)]">
          <Metric label="Asks · 30d" value={s.asks_30} sub={`${answerRate(s)} answered`} />
          <Metric
            label="Client errors · 30d"
            value={sum(s.errors)}
            tone={sum(s.errors) > 0 ? 'danger' : 'muted'}
            sub={s.errors_by_kind.map((k) => `${k.count} ${k.kind}`).join(' · ') || undefined}
          />
          <Metric label="Stale pages" value={s.stale_pages} sub="90d+ untouched" tone={s.stale_pages > 0 ? 'warning' : 'muted'} />
          <Metric label="Orphan pages" value={s.orphan_pages} sub="no inbound links" tone={s.orphan_pages > 0 ? 'warning' : 'muted'} />
          <Metric label="Contradictions" value={s.contradictions} tone={s.contradictions > 0 ? 'warning' : 'muted'} />
        </div>
      </Group>

      {/* Unanswered questions — what people asked that the docs couldn't answer */}
      <Group title="Unanswered questions">
        <TopList
          title="Recent asks that returned nothing — a to-do list for what to write"
          empty="Every recent question found something. 🎉"
        >
          {s.unanswered_asks.map((a, i) => (
            <UnansweredRow key={i} a={a} />
          ))}
        </TopList>
      </Group>

      <AIUsageSection />
    </section>
  )
}

function SignupRow({ u }: { u: StatsSignup }) {
  return (
    <li className="flex items-baseline justify-between gap-[var(--space-2)] text-[length:var(--text-sm)]">
      <span className="min-w-0 truncate text-[var(--text-primary)]">
        <span className="font-medium">{u.display_name || u.username}</span>
        {u.email ? <span className="text-[var(--text-muted)]"> · {u.email}</span> : null}
      </span>
      <span className="flex shrink-0 items-baseline gap-[var(--space-2)] text-[length:var(--text-xs)] text-[var(--text-muted)]">
        <span className={u.activated ? 'text-[var(--accent)]' : 'text-[var(--text-muted)]'}>
          {u.activated ? 'active' : 'no activity'}
        </span>
        <span className="tabular-nums">{relativeTimeFromSqlite(u.created_at)}</span>
      </span>
    </li>
  )
}

function UnansweredRow({ a }: { a: StatsUnanswered }) {
  return (
    <li className="flex flex-col gap-[1px] text-[length:var(--text-sm)]">
      <span className="text-[var(--text-primary)]">{a.question}</span>
      <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">
        {a.who ? `${a.who} · ` : ''}
        {relativeTimeFromSqlite(a.created_at)}
      </span>
    </li>
  )
}

// AI usage / token volume — the basis for cost estimation. Loads independently
// of the main stats query. Token counts are estimates (~chars/4).
function AIUsageSection() {
  const q = useAIUsage()
  if (q.isLoading || q.isError || !q.data) return null
  const { weeks, models } = q.data
  if (weeks.length === 0 && models.length === 0) {
    return (
      <Group title="AI usage (token volume)">
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          No AI usage recorded yet — it accrues as chat, embedding, and image
          calls run.
        </p>
      </Group>
    )
  }
  return (
    <Group title="AI usage · token volume (estimated)">
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-[var(--space-4)]">
        <div className="flex flex-col gap-[var(--space-2)] rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--surface-1)] p-[var(--space-3)] overflow-x-auto">
          <span className="text-[length:var(--text-xs)] font-medium uppercase tracking-wide text-[var(--text-muted)]">
            By week
          </span>
          <table className="w-full text-[length:var(--text-sm)] tabular-nums">
            <thead>
              <tr className="text-[var(--text-muted)] text-left text-[length:var(--text-xs)]">
                <th className="font-medium pr-[var(--space-3)]">Week</th>
                <th className="font-medium pr-[var(--space-3)] text-right">Chat tok</th>
                <th className="font-medium pr-[var(--space-3)] text-right">Embed tok</th>
                <th className="font-medium text-right">Images</th>
              </tr>
            </thead>
            <tbody>
              {weeks.map((w) => (
                <tr key={w.week} className="text-[var(--text-primary)]">
                  <td className="pr-[var(--space-3)] text-[var(--text-muted)]">{w.week}</td>
                  <td className="pr-[var(--space-3)] text-right">{w.chat_tokens.toLocaleString()}</td>
                  <td className="pr-[var(--space-3)] text-right">{w.embed_tokens.toLocaleString()}</td>
                  <td className="text-right">{w.images.toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="flex flex-col gap-[var(--space-2)] rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--surface-1)] p-[var(--space-3)]">
          <span className="text-[length:var(--text-xs)] font-medium uppercase tracking-wide text-[var(--text-muted)]">
            By model (12 weeks)
          </span>
          <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
            {models.map((m) => (
              <Row
                key={`${m.model}:${m.kind}`}
                left={
                  <>
                    <span className="font-medium">{m.model || '(default)'}</span>{' '}
                    <span className="text-[var(--text-muted)]">· {m.kind} · {m.calls.toLocaleString()} calls</span>
                  </>
                }
                count={m.kind === 'image' ? m.units : m.tokens}
              />
            ))}
          </ul>
        </div>
      </div>
      <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)]">
        Token counts are estimates (~4 chars/token). Apply a per-provider price
        table to these for a cost estimate.
      </p>
    </Group>
  )
}

function answerRate(s: AdminStats): string {
  if (s.asks_30 === 0) return '0%'
  return `${Math.round((s.asks_answered_30 / s.asks_30) * 100)}%`
}

const TONE: Record<string, string> = {
  accent: 'text-[var(--accent)]',
  danger: 'text-[var(--danger)]',
  warning: 'text-[var(--warning)]',
  muted: 'text-[var(--text-muted)]',
}

function Metric({
  label,
  value,
  series,
  sub,
  tone = 'muted',
}: {
  label: string
  value: number
  series?: number[]
  sub?: string
  tone?: 'accent' | 'danger' | 'warning' | 'muted'
}) {
  return (
    <div className="flex flex-col gap-[var(--space-1)] min-w-0 overflow-hidden rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--surface-1)] p-[var(--space-3)]">
      <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">{label}</span>
      <span className="text-[length:var(--text-xl)] font-semibold text-[var(--text-primary)] tabular-nums leading-none">
        {value.toLocaleString()}
      </span>
      {series && series.length > 0 ? (
        <span className={cn('block w-full', TONE[tone])}>
          <Sparkline values={series} width={160} height={28} ariaLabel={`${label} trend`} />
        </span>
      ) : null}
      {sub ? <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] truncate">{sub}</span> : null}
    </div>
  )
}

function Group({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-[var(--space-3)]">
      <h2 className="m-0 text-[length:var(--text-sm)] font-semibold text-[var(--text-primary)]">{title}</h2>
      {children}
    </div>
  )
}

function TopList({ title, empty, children }: { title: string; empty: string; children: React.ReactNode[] }) {
  return (
    <div className="flex flex-col gap-[var(--space-2)] rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--surface-1)] p-[var(--space-3)]">
      <span className="text-[length:var(--text-xs)] font-medium uppercase tracking-wide text-[var(--text-muted)]">
        {title}
      </span>
      {children.length > 0 ? (
        <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">{children}</ul>
      ) : (
        <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">{empty}</span>
      )}
    </div>
  )
}

function Row({ left, count }: { left: React.ReactNode; count: number }) {
  return (
    <li className="flex items-baseline justify-between gap-[var(--space-2)] text-[length:var(--text-sm)]">
      <span className="min-w-0 truncate text-[var(--text-primary)]">{left}</span>
      <span className="shrink-0 tabular-nums text-[var(--text-muted)]">{count.toLocaleString()}</span>
    </li>
  )
}

function PageRow({ p }: { p: StatsTopPage }) {
  return (
    <Row
      left={
        <button
          type="button"
          onClick={() => navigateToPage(p.space_id, p.page_id)}
          className="reader-meta-link text-left"
        >
          {p.title}{' '}
          <span className="text-[var(--text-muted)]">· {p.space_name}</span>
        </button>
      }
      count={p.count}
    />
  )
}

function SpaceRow({ s }: { s: StatsTopSpace }) {
  return <Row left={s.name} count={s.count} />
}

function LabelRow({ label, count }: StatsTopPerson) {
  return <Row left={<span className="font-medium">{label}</span>} count={count} />
}
