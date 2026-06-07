import { useMemo } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import {
  Clock,
  FileClock,
  FilePlus,
  Network,
  PencilLine,
  Search,
  Star,
  Building2,
} from 'lucide-react'
import { useMe } from '../../lib/queries/auth'
import { useRecentChanges } from '../../lib/queries/recent-changes'
import { useFavorites } from '../../lib/queries/favorites'
import { useSpaces } from '../../lib/queries/spaces'
import { readRecentPages } from '../../lib/recentPages'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import { emitOpenNewPage } from '../../lib/newPageEvent'
import { emitOpenPalette } from '../../lib/paletteEvent'
import { Button } from '../ui/button'
import { cn } from '../../lib/utils'

// Home dashboard — the app's landing surface (mounted at `/`). A launchpad plus
// three lenses on the wiki: what the team changed, what you changed, and what
// you've starred / visited. New widgets (notifications, drafts) slot in here.
export function HomeRoute() {
  const me = useMe()
  const spaces = useSpaces()
  const recent = useRecentChanges()
  const myEdits = useRecentChanges({ mine: true })
  const favorites = useFavorites()
  const visited = useMemo(() => readRecentPages(), [])

  const spaceCount = spaces.data?.length ?? 0
  const favCount = favorites.data?.length ?? 0
  const noSpaces = spaces.isSuccess && spaceCount === 0

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-[64rem] w-full mx-auto p-[var(--space-7)] flex flex-col gap-[var(--space-6)]">
        <header className="flex flex-col gap-[var(--space-2)]">
          <h1 className="m-0 font-[family-name:var(--font-sans)] text-[length:var(--text-2xl)] leading-[var(--leading-tight)] text-[var(--text-primary)]">
            {me.data?.username ? `Welcome back, ${me.data.username}` : 'Home'}
          </h1>
          <div className="flex items-center gap-[var(--space-3)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
            <Stat label={spaceCount === 1 ? 'space' : 'spaces'} value={spaceCount} />
            <span aria-hidden>·</span>
            <Stat label={favCount === 1 ? 'favorite' : 'favorites'} value={favCount} />
          </div>
        </header>

        {/* Quick actions launchpad. */}
        <div className="flex flex-wrap gap-[var(--space-2)]">
          <Button variant="primary" size="sm" onClick={() => emitOpenNewPage()}>
            <FilePlus width={14} height={14} />
            <span>New page</span>
          </Button>
          <Button asChild variant="secondary" size="sm">
            <Link to="/n">
              <PencilLine width={14} height={14} />
              <span>Quick note</span>
            </Link>
          </Button>
          <Button variant="secondary" size="sm" onClick={() => emitOpenPalette('pages')}>
            <Search width={14} height={14} />
            <span>Search</span>
          </Button>
          <Button asChild variant="secondary" size="sm">
            <Link to="/graph">
              <Network width={14} height={14} />
              <span>Graph</span>
            </Link>
          </Button>
        </div>

        {noSpaces ? (
          <FirstRunEmptyState />
        ) : (
          <>
            <SpacesGrid />

            <div className="grid grid-cols-1 lg:grid-cols-3 gap-[var(--space-5)]">
              <Widget
                className="lg:col-span-2"
                icon={FileClock}
                title="Recent changes"
                loading={recent.isLoading}
                error={recent.isError}
                empty={!recent.data || recent.data.length === 0}
                emptyText="No edits yet. Create or edit a page and it shows up here."
              >
                {recent.data?.map((c) => (
                  <PageRow
                    key={`change-${c.page_id}`}
                    spaceId={c.space_id}
                    pageId={c.page_id}
                    title={c.title}
                    meta={`${c.space_name} · ${
                      c.author_username ? `${c.author_username} · ` : ''
                    }${relativeTimeFromSqlite(c.updated_at)}`}
                  />
                ))}
              </Widget>

              <div className="flex flex-col gap-[var(--space-5)]">
                <Widget
                  icon={PencilLine}
                  title="My recent edits"
                  loading={myEdits.isLoading}
                  error={myEdits.isError}
                  empty={!myEdits.data || myEdits.data.length === 0}
                  emptyText="Pages you edit will appear here."
                >
                  {myEdits.data?.map((c) => (
                    <PageRow
                      key={`mine-${c.page_id}`}
                      spaceId={c.space_id}
                      pageId={c.page_id}
                      title={c.title}
                      meta={`${c.space_name} · ${relativeTimeFromSqlite(c.updated_at)}`}
                    />
                  ))}
                </Widget>

                <Widget
                  icon={Star}
                  title="Favorites"
                  loading={favorites.isLoading}
                  error={favorites.isError}
                  empty={!favorites.data || favorites.data.length === 0}
                  emptyText="Star a page to pin it here."
                >
                  {favorites.data?.map((f) => (
                    <PageRow
                      key={`fav-${f.page_id}`}
                      spaceId={f.space_id}
                      pageId={f.page_id}
                      title={f.title}
                      meta={f.space_name}
                    />
                  ))}
                </Widget>

                <Widget
                  icon={Clock}
                  title="Recently visited"
                  empty={visited.length === 0}
                  emptyText="Pages you open will appear here."
                >
                  {visited.map((v) => (
                    <PageRow
                      key={`visited-${v.pageId}`}
                      spaceId={v.spaceId}
                      pageId={v.pageId}
                      title={v.title}
                    />
                  ))}
                </Widget>
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <span>
      <span className="text-[var(--text-primary)] font-medium">{value}</span> {label}
    </span>
  )
}

// Spaces you can reach, as jump-in cards (more context than the sidebar list).
function SpacesGrid() {
  const spaces = useSpaces()
  if (!spaces.data || spaces.data.length === 0) return null
  return (
    <section className="flex flex-col gap-[var(--space-3)]">
      <h2 className="m-0 flex items-center gap-[var(--space-2)] text-[length:var(--text-sm)] font-semibold text-[var(--text-primary)] font-[family-name:var(--font-sans)]">
        <Building2 width={16} height={16} aria-hidden className="text-[var(--text-muted)]" />
        Your spaces
      </h2>
      <ul className="m-0 p-0 list-none grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-[var(--space-3)]">
        {spaces.data.map((s) => (
          <li key={s.id}>
            <Link
              to="/spaces/$spaceId"
              params={{ spaceId: s.id }}
              className={cn(
                'flex flex-col gap-[var(--space-1)] p-[var(--space-4)] h-full',
                'rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-1)]',
                'no-underline transition-colors duration-[var(--duration-fast)]',
                'hover:border-[var(--border-strong)] hover:bg-[var(--surface-2)]',
              )}
            >
              <span className="truncate text-[length:var(--text-sm)] font-medium text-[var(--text-primary)]">
                {s.name}
              </span>
              <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">
                {s.is_personal
                  ? 'Personal'
                  : `${s.member_count ?? 0} ${(s.member_count ?? 0) === 1 ? 'member' : 'members'}`}
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  )
}

function FirstRunEmptyState() {
  const navigate = useNavigate()
  return (
    <section
      className={cn(
        'flex flex-col items-start gap-[var(--space-3)] p-[var(--space-6)]',
        'rounded-[var(--radius-lg)] border border-[var(--border-subtle)] bg-[var(--surface-1)]',
      )}
    >
      <h2 className="m-0 text-[length:var(--text-lg)] font-semibold text-[var(--text-primary)]">
        Make your first space
      </h2>
      <p className="m-0 max-w-[40rem] text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
        A space is a tree of markdown pages your team writes together. Create one,
        and we’ll drop in a short Welcome page to get you going.
      </p>
      <Button variant="primary" size="sm" onClick={() => void navigate({ to: '/n' })}>
        <PencilLine width={14} height={14} />
        <span>Start a quick note</span>
      </Button>
    </section>
  )
}

function Widget({
  icon: Icon,
  title,
  loading,
  error,
  empty,
  emptyText,
  className,
  children,
}: {
  icon: typeof Star
  title: string
  loading?: boolean
  error?: boolean
  empty?: boolean
  emptyText: string
  className?: string
  children?: React.ReactNode
}) {
  return (
    <section
      className={cn(
        'flex flex-col gap-[var(--space-3)] p-[var(--space-5)]',
        'rounded-[var(--radius-lg)] border border-[var(--border-subtle)] bg-[var(--surface-1)]',
        className,
      )}
    >
      <h2 className="m-0 flex items-center gap-[var(--space-2)] text-[length:var(--text-sm)] font-semibold text-[var(--text-primary)] font-[family-name:var(--font-sans)]">
        <Icon width={16} height={16} aria-hidden className="text-[var(--text-muted)]" />
        {title}
      </h2>
      {loading ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">Loading…</p>
      ) : error ? (
        <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
          Couldn’t load.
        </p>
      ) : empty ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">{emptyText}</p>
      ) : (
        <ul className="m-0 p-0 list-none flex flex-col gap-[1px]">{children}</ul>
      )}
    </section>
  )
}

function PageRow({
  spaceId,
  pageId,
  title,
  meta,
}: {
  spaceId: number
  pageId: number
  title: string
  meta?: React.ReactNode
}) {
  return (
    <li>
      <Link
        to="/spaces/$spaceId/pages/$pageId/{-$slug}"
        params={{ spaceId, pageId, slug: undefined }}
        className={cn(
          'flex flex-col gap-[1px] px-[var(--space-3)] py-[var(--space-2)]',
          'rounded-[var(--radius-sm)] no-underline',
          'hover:bg-[var(--surface-2)]',
        )}
      >
        <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)]">
          {title || 'Untitled'}
        </span>
        {meta ? (
          <span className="truncate text-[length:var(--text-xs)] text-[var(--text-muted)]">
            {meta}
          </span>
        ) : null}
      </Link>
    </li>
  )
}
