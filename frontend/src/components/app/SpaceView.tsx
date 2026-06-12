import { useMemo } from 'react'
import {
  CalendarClock,
  Clock,
  Copy,
  FileText,
  Folder,
  ShieldAlert,
  Unlink,
} from 'lucide-react'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../ui/tabs'
import { EmptyState } from '../ui/empty-state'
import { useSpaceOverview } from '../../lib/queries/space-overview'
import { useSpaces } from '../../lib/queries/spaces'
import { navigateToPage } from '../../lib/pageHitItem'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import { cn } from '../../lib/utils'

// The per-space landing (fills the old "select a page" placeholder). Two tabs:
// Overview — a content-first hub (top-level pages + recent activity); and Health —
// the per-space maintenance worklist (disputes, review-overdue, orphans, dupes).
// Read-only: every row just navigates; nothing here authors.
export function SpaceView({ spaceId }: { spaceId: number }) {
  const { data, isLoading } = useSpaceOverview(spaceId)
  const spacesQuery = useSpaces()
  const spaceName = useMemo(
    () => spacesQuery.data?.find((s) => s.id === spaceId)?.name ?? 'Space',
    [spacesQuery.data, spaceId],
  )

  const h = data?.health
  const issues =
    (h?.disputed.length ?? 0) +
    (h?.review_overdue.length ?? 0) +
    (h?.orphans.length ?? 0) +
    (h?.duplicates.length ?? 0)

  return (
    <div className="flex-1 overflow-y-auto min-h-0">
      <div className="flex flex-col gap-[var(--space-5)] p-[var(--space-7)] max-w-[56rem] w-full mx-auto">
        <header className="flex items-baseline gap-[var(--space-3)]">
          <h1 className="m-0 text-[length:var(--text-3xl)] leading-[var(--leading-tight)] font-medium text-[var(--text-primary)] font-[family-name:var(--font-sans)]">
            {spaceName}
          </h1>
          {data ? (
            <span className="text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
              {data.pages} {data.pages === 1 ? 'page' : 'pages'}
            </span>
          ) : null}
        </header>

        <Tabs defaultValue="overview" className="flex flex-col gap-[var(--space-4)]">
          <TabsList>
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="health">
              <span className="inline-flex items-center gap-[var(--space-2)]">
                Health
                {issues > 0 ? (
                  <span className="inline-flex items-center justify-center min-w-[var(--space-5)] h-[var(--space-5)] px-[var(--space-1)] rounded-full text-[length:var(--text-xs)] bg-[color-mix(in_oklch,var(--danger)_18%,transparent)] text-[var(--danger)]">
                    {issues}
                  </span>
                ) : null}
              </span>
            </TabsTrigger>
          </TabsList>

          {/* ---- Overview ---- */}
          <TabsContent value="overview" className="flex flex-col gap-[var(--space-6)]">
            {isLoading ? (
              <Skeleton />
            ) : (
              <>
                <Section title="Contents" icon={Folder}>
                  {data && data.top_level.length > 0 ? (
                    <List>
                      {data.top_level.map((p) => (
                        <Row
                          key={p.id}
                          icon={FileText}
                          title={p.title || 'Untitled'}
                          meta={
                            p.children > 0
                              ? `${p.children} ${p.children === 1 ? 'page' : 'pages'}`
                              : undefined
                          }
                          onSelect={() => navigateToPage(spaceId, p.id)}
                        />
                      ))}
                    </List>
                  ) : (
                    <Muted>No top-level pages yet.</Muted>
                  )}
                </Section>

                {data && data.recent.length > 0 ? (
                  <Section title="Recently updated" icon={Clock}>
                    <List>
                      {data.recent.map((p) => (
                        <Row
                          key={p.id}
                          icon={FileText}
                          title={p.title || 'Untitled'}
                          meta={p.updated_at ? relativeTimeFromSqlite(p.updated_at) : undefined}
                          onSelect={() => navigateToPage(spaceId, p.id)}
                        />
                      ))}
                    </List>
                  </Section>
                ) : null}
              </>
            )}
          </TabsContent>

          {/* ---- Health ---- */}
          <TabsContent value="health" className="flex flex-col gap-[var(--space-6)]">
            {isLoading ? (
              <Skeleton />
            ) : issues === 0 ? (
              <EmptyState
                icon={ShieldAlert}
                title="This space looks healthy"
                description="No disputes, orphans, overdue reviews, or likely duplicates right now."
              />
            ) : (
              <>
                {h && h.disputed.length > 0 ? (
                  <Section title="Disputed" icon={ShieldAlert} tone="danger">
                    <List>
                      {h.disputed.map((d) => (
                        <Row
                          key={d.id}
                          icon={ShieldAlert}
                          tone="danger"
                          title={d.title || 'Untitled'}
                          meta={`${d.n} conflict${d.n === 1 ? '' : 's'}`}
                          onSelect={() => navigateToPage(spaceId, d.id)}
                        />
                      ))}
                    </List>
                  </Section>
                ) : null}

                {h && h.review_overdue.length > 0 ? (
                  <Section title="Needs review" icon={CalendarClock} tone="warning">
                    <List>
                      {h.review_overdue.map((p) => (
                        <Row
                          key={p.id}
                          icon={CalendarClock}
                          tone="warning"
                          title={p.title || 'Untitled'}
                          meta={`due — every ${p.every}d, ${p.age_days}d old`}
                          onSelect={() => navigateToPage(spaceId, p.id)}
                        />
                      ))}
                    </List>
                  </Section>
                ) : null}

                {h && h.orphans.length > 0 ? (
                  <Section title="Orphans" icon={Unlink}>
                    <List>
                      {h.orphans.map((p) => (
                        <Row
                          key={p.id}
                          icon={Unlink}
                          title={p.title || 'Untitled'}
                          meta="no links"
                          onSelect={() => navigateToPage(spaceId, p.id)}
                        />
                      ))}
                    </List>
                  </Section>
                ) : null}

                {h && h.duplicates.length > 0 ? (
                  <Section title="Possible duplicates" icon={Copy}>
                    <List>
                      {h.duplicates.map((d, i) => (
                        <Row
                          key={`${d.page_a}-${d.page_b}-${i}`}
                          icon={Copy}
                          title={`${d.title_a || 'Untitled'}  ·  ${d.title_b || 'Untitled'}`}
                          meta={`${Math.round(d.similarity * 100)}% alike`}
                          onSelect={() => navigateToPage(spaceId, d.page_a)}
                        />
                      ))}
                    </List>
                  </Section>
                ) : null}
              </>
            )}
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}

function Section({
  title,
  icon: Icon,
  tone,
  children,
}: {
  title: string
  icon: typeof FileText
  tone?: 'danger' | 'warning'
  children: React.ReactNode
}) {
  const color =
    tone === 'danger' ? 'var(--danger)' : tone === 'warning' ? 'var(--warning)' : 'var(--text-muted)'
  return (
    <section className="flex flex-col gap-[var(--space-2)]">
      <h2
        className="m-0 flex items-center gap-[var(--space-2)] text-[length:var(--text-xs)] uppercase tracking-wider font-[family-name:var(--font-sans)]"
        style={{ color }}
      >
        <Icon width={13} height={13} aria-hidden />
        {title}
      </h2>
      {children}
    </section>
  )
}

function List({ children }: { children: React.ReactNode }) {
  return <ul className="m-0 p-0 list-none flex flex-col gap-[1px]">{children}</ul>
}

function Row({
  icon: Icon,
  title,
  meta,
  tone,
  onSelect,
}: {
  icon: typeof FileText
  title: string
  meta?: string
  tone?: 'danger' | 'warning'
  onSelect: () => void
}) {
  return (
    <li className="m-0 p-0 list-none">
      <button
        type="button"
        onClick={onSelect}
        className={cn(
          'group w-full text-left flex items-center gap-[var(--space-3)]',
          'px-[var(--space-3)] py-[var(--space-2)] rounded-[var(--radius-sm)]',
          'bg-transparent border-0 cursor-pointer outline-none',
          'hover:bg-[var(--surface-2)] focus-visible:ring-2 focus-visible:ring-[var(--accent)]',
        )}
      >
        <Icon
          width={14}
          height={14}
          aria-hidden
          className="shrink-0"
          style={{
            color:
              tone === 'danger'
                ? 'var(--danger)'
                : tone === 'warning'
                  ? 'var(--warning)'
                  : 'var(--text-muted)',
          }}
        />
        <span className="flex-1 min-w-0 truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium font-[family-name:var(--font-sans)]">
          {title}
        </span>
        {meta ? (
          <span className="shrink-0 text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
            {meta}
          </span>
        ) : null}
      </button>
    </li>
  )
}

function Muted({ children }: { children: React.ReactNode }) {
  return (
    <p className="m-0 px-[var(--space-3)] text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
      {children}
    </p>
  )
}

function Skeleton() {
  return (
    <div className="flex flex-col gap-[var(--space-2)]">
      <div className="h-[var(--space-6)] w-1/3 rounded-[var(--radius-sm)] bg-[var(--surface-2)]" />
      <div className="h-[calc(var(--space-8)*2)] rounded-[var(--radius-md)] bg-[var(--surface-2)]" />
    </div>
  )
}
