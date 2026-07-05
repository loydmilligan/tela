import { useEffect, useMemo, useRef, useState } from 'react'
import {
  EVENT_TYPE_GROUPS,
  useInfiniteEvents,
  type EventFilters,
} from '../../lib/queries/events'
import { EventRow, collapseEvents } from './EventRow'
import { IncludeAdminsToggle } from './IncludeAdminsToggle'
import { Toggle } from '../ui/toggle'
import { Input } from '../ui/input'
import { Button } from '../ui/button'
import { cn } from '../../lib/utils'

// Instance-admin activity feed: every login, page view/edit, access change, ask,
// and API request, newest-first, with type/search/date filters and keyset-based
// infinite scroll (the first useInfiniteQuery in the app).
export function SettingsEventsTab() {
  // Selected group labels (empty = all). Each group maps to one or more type
  // tokens passed to the backend.
  const [groups, setGroups] = useState<Set<string>>(new Set())
  const [search, setSearch] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const [since, setSince] = useState('')
  const [includeAdmins, setIncludeAdmins] = useState(false)

  // Debounce the free-text box so each keystroke doesn't refetch.
  useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(search), 250)
    return () => clearTimeout(t)
  }, [search])

  const filters = useMemo<EventFilters>(() => {
    const types = EVENT_TYPE_GROUPS.filter((g) => groups.has(g.label)).flatMap(
      (g) => g.types,
    )
    return {
      types: types.length > 0 ? types : undefined,
      q: debouncedSearch.trim() || undefined,
      since: since || undefined,
      includeAdmins: includeAdmins || undefined,
    }
  }, [groups, debouncedSearch, since, includeAdmins])

  const query = useInfiniteEvents(filters)
  const {
    data,
    isLoading,
    isError,
    hasNextPage,
    isFetchingNextPage,
    fetchNextPage,
  } = query
  const events = useMemo(
    () => data?.pages.flatMap((p) => p.events) ?? [],
    [data],
  )
  // Collapse consecutive identical events (e.g. a burst of autosave edits on one
  // page by one user) into single "×N" rows so the feed stays readable.
  const eventGroups = useMemo(() => collapseEvents(events), [events])

  // Infinite scroll: when the sentinel scrolls into view and there's another
  // page, fetch it. fetchNextPage is referentially stable across renders.
  const sentinelRef = useRef<HTMLDivElement | null>(null)
  useEffect(() => {
    const el = sentinelRef.current
    if (!el) return
    const io = new IntersectionObserver((entries) => {
      if (entries[0]?.isIntersecting && hasNextPage && !isFetchingNextPage) {
        void fetchNextPage()
      }
    })
    io.observe(el)
    return () => io.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  function toggleGroup(label: string) {
    setGroups((prev) => {
      const next = new Set(prev)
      if (next.has(label)) next.delete(label)
      else next.add(label)
      return next
    })
  }

  const hasFilters =
    groups.size > 0 || search !== '' || since !== '' || includeAdmins

  return (
    <section
      aria-labelledby="settings-events"
      className="flex flex-col gap-[var(--space-4)] min-h-0"
    >
      <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
        Everything happening on this instance — sign-ins, page views and edits,
        access changes, asks, and API requests. Most recent first.
      </p>

      {/* Filter bar */}
      <div className="flex flex-col gap-[var(--space-3)]">
        <div className="flex flex-wrap items-center gap-[var(--space-2)]">
          {EVENT_TYPE_GROUPS.map((g) => (
            <Toggle
              key={g.label}
              size="sm"
              pressed={groups.has(g.label)}
              onPressedChange={() => toggleGroup(g.label)}
            >
              {g.label}
            </Toggle>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-[var(--space-3)]">
          <Input
            type="search"
            placeholder="Search actor, page, detail…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="max-w-[18rem]"
          />
          <label className="flex items-center gap-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
            Since
            <Input
              type="date"
              value={since}
              onChange={(e) => setSince(e.target.value)}
              className="max-w-[10rem]"
            />
          </label>
          <IncludeAdminsToggle
            checked={includeAdmins}
            onChange={setIncludeAdmins}
          />
          {hasFilters ? (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setGroups(new Set())
                setSearch('')
                setSince('')
                setIncludeAdmins(false)
              }}
            >
              Clear
            </Button>
          ) : null}
        </div>
      </div>

      {/* Feed */}
      {isLoading ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          Loading events…
        </p>
      ) : isError ? (
        <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
          Couldn't load events.
        </p>
      ) : events.length > 0 ? (
        <>
          <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
            {eventGroups.map((g) => (
              <EventRow
                key={g.head.id}
                event={g.head}
                count={g.count}
                oldestAt={g.oldestAt}
              />
            ))}
          </ul>
          <div ref={sentinelRef} aria-hidden className="h-[var(--space-6)]" />
          <p
            className={cn(
              'm-0 text-center text-[length:var(--text-xs)] text-[var(--text-muted)]',
              !isFetchingNextPage && 'invisible',
            )}
          >
            Loading more…
          </p>
        </>
      ) : (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          {hasFilters ? 'No events match these filters.' : 'No events recorded yet.'}
        </p>
      )}
    </section>
  )
}
