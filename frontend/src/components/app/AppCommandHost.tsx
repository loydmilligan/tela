import { useCallback, useEffect, useMemo, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Clock, FileText } from 'lucide-react'
import {
  CommandPalette,
  prefixForMode,
  usePaletteShortcuts,
  type CommandItem,
  type CommandMode,
  type CommandSubPicker,
} from '../ui/command'
import { useSpaces } from '../../lib/queries/spaces'
import { router } from '../../routes/router'
import {
  getTheme,
  setTheme,
  subscribeToTheme,
  type ThemeName,
} from '../../lib/theme'
import {
  materializeCommands,
  type CommandContext,
  type SubPickerSpec,
} from '../../lib/commands'
// Side-effect: starter commands self-register on import. Keep this import in
// the app-level host so the registry is populated before the palette mounts.
import '../../lib/commands/starters'
import { readLastPage } from '../../lib/lastPage'
import { subscribeToOpenNewPage } from '../../lib/newPageEvent'
import { NewPageDialog } from './NewPageDialog'
import { searchPages, type SearchResult } from '../../lib/api'
import { useDebouncedValue } from '../../lib/useDebouncedValue'
import { readRecentPages, type RecentPage } from '../../lib/recentPages'
import { HighlightedSnippet } from '../../lib/highlightSnippet'

// Reactive view of the active theme so commands that read currentTheme always
// see the freshest value. Subscribes to setTheme() broadcasts.
function useThemeName(): ThemeName {
  const [theme, setLocal] = useState<ThemeName>(() => getTheme())
  useEffect(() => subscribeToTheme(setLocal), [])
  return theme
}

// Read current route context imperatively. AppCommandHost lives outside the
// RouterProvider, so it can't use useParams; instead it reaches into the
// router instance at the moment the dialog opens. The deepest match wins:
//   /spaces/$spaceId/pages/$pageId  -> {spaceId, pageId}
//   /spaces/$spaceId               -> {spaceId, pageId: null}
//   /                              -> {spaceId: null, pageId: null}
function readRouteContext(): {
  spaceId: number | null
  pageId: number | null
} {
  let spaceId: number | null = null
  let pageId: number | null = null
  for (const m of router.state.matches) {
    const p = m.params as { spaceId?: number; pageId?: number }
    if (typeof p.spaceId === 'number') spaceId = p.spaceId
    if (typeof p.pageId === 'number') pageId = p.pageId
  }
  return { spaceId, pageId }
}

// Search debounce target. 50ms is fast enough that the user can't perceive a
// gap between stop-typing and result-arrival, but slow enough that rapid
// typing collapses into a single in-flight request rather than one per key.
const SEARCH_DEBOUNCE_MS = 50
const RECENTS_VISIBLE = 8

function navigateToPage(spaceId: number, pageId: number) {
  void router.navigate({
    to: '/spaces/$spaceId/pages/$pageId',
    params: { spaceId, pageId },
  })
}

// App-level palette mount. Owns:
//  - palette open / sub-picker / external-search-request state
//  - the global keyboard contract (Cmd-K, Cmd-Shift-P, Cmd-N)
//  - the CommandContext that registered commands run against
//  - the M4.2 new-page dialog (Cmd-N + sidebar "+ New page" button + command)
//  - the M5.1 pages-mode server search (debounced /api/search) and the
//    recently-viewed empty-state list
//
// Sits outside RouterProvider in App.tsx (sibling to it), so navigation goes
// through the imported `router` instance rather than useNavigate. Lives inside
// QueryClientProvider so useSpaces() / useQuery resolve from the shared cache.
export function AppCommandHost() {
  const [open, setOpen] = useState(false)
  const [initialMode, setInitialMode] = useState<CommandMode>('pages')
  const [subPicker, setSubPicker] = useState<CommandSubPicker | null>(null)
  const [searchRequest, setSearchRequest] =
    useState<{ value: string; nonce: number } | null>(null)

  // Pages-mode query: the palette pushes the current query here via
  // onPagesQueryChange; the host debounces and turns it into /api/search.
  const [pagesQuery, setPagesQuery] = useState('')
  const debouncedQuery = useDebouncedValue(pagesQuery, SEARCH_DEBOUNCE_MS)
  const trimmedQuery = debouncedQuery.trim()

  // Snapshot recently-viewed pages each time the palette opens so a navigation
  // away and back surfaces the freshly-visited page without remounting the
  // palette. Window 'storage' events would catch cross-tab writes but those
  // aren't a real scenario for single-user v0.
  const [recents, setRecents] = useState<RecentPage[]>([])
  useEffect(() => {
    if (open) setRecents(readRecentPages().slice(0, RECENTS_VISIBLE))
  }, [open])

  // New-page dialog state + pre-fill defaults snapshotted at open time so
  // route changes after open don't retroactively re-target the dialog.
  const [newPageOpen, setNewPageOpen] = useState(false)
  const [newPageDefaults, setNewPageDefaults] = useState<{
    spaceId: number | null
    parentId: number | null
  }>({ spaceId: null, parentId: null })

  const currentTheme = useThemeName()
  const spacesQuery = useSpaces()
  const spaces = spacesQuery.data ?? []

  // Server-side title+body search. `enabled` gates the empty query so we don't
  // burn a round-trip; `placeholderData: keepPreviousData` keeps stale results
  // visible while a new query is in flight so the list doesn't flicker on
  // every keystroke. TanStack Query auto-cancels the in-flight fetch when the
  // queryKey changes — that's how rapid typing only renders the final result.
  const searchQuery = useQuery<SearchResult[]>({
    queryKey: ['search', trimmedQuery],
    queryFn: ({ signal }) =>
      searchPages(trimmedQuery, signal).then((r) => r.results),
    enabled: trimmedQuery.length > 0,
    staleTime: 30_000,
    gcTime: 5 * 60_000,
    placeholderData: keepPreviousData,
  })

  // Clear transient state whenever the palette closes so the next open starts
  // fresh — no stale sub-picker, no leftover external search push, no zombie
  // pages query feeding a search round-trip.
  const handleOpenChange = useCallback((next: boolean) => {
    setOpen(next)
    if (!next) {
      setSubPicker(null)
      setSearchRequest(null)
      setPagesQuery('')
    }
  }, [])

  const openWith = useCallback((mode: CommandMode) => {
    setSubPicker(null)
    setSearchRequest(null)
    setInitialMode(mode)
    setOpen(true)
  }, [])

  const openNewPage = useCallback(() => {
    const ctx = readRouteContext()
    let seedSpace = ctx.spaceId
    if (seedSpace == null) {
      // At app root — fall back to the last-viewed space, then the first
      // available. Resolved against the loaded spaces list when applicable.
      const last = readLastPage()
      if (last && spaces.some((s) => s.id === last.spaceId)) {
        seedSpace = last.spaceId
      } else {
        seedSpace = spaces[0]?.id ?? null
      }
    }
    // Parent only pre-fills when we're standing on an actual page; never carry
    // a parent into the dialog from somewhere other than a page view.
    const seedParent = ctx.spaceId != null ? ctx.pageId : null
    setNewPageDefaults({ spaceId: seedSpace, parentId: seedParent })
    setNewPageOpen(true)
  }, [spaces])

  // Sidebar "+ New page" button (and any future caller) dispatches this event
  // to ask the host to open the dialog. Window event keeps the bridge simple
  // across the RouterProvider boundary.
  useEffect(() => subscribeToOpenNewPage(openNewPage), [openNewPage])

  usePaletteShortcuts({
    onOpenPages: () => openWith('pages'),
    onOpenCommands: () => openWith('commands'),
    onNewPage: openNewPage,
  })

  const ctx = useMemo<CommandContext>(
    () => ({
      currentTheme,
      setTheme,
      spaces,
      navigateToSpace: (spaceId) => {
        void router.navigate({
          to: '/spaces/$spaceId',
          params: { spaceId },
        })
      },
      openHelpMode: () => {
        // Push the help prefix into the open palette via searchRequest. Nonce
        // forces the effect to fire even when the same value is sent twice.
        setSubPicker(null)
        setSearchRequest((prev) => ({
          value: prefixForMode('help'),
          nonce: (prev?.nonce ?? 0) + 1,
        }))
      },
      openSubPicker: (spec: SubPickerSpec) => {
        setSubPicker({
          label: spec.label,
          placeholder: spec.placeholder,
          emptyMessage: spec.emptyMessage,
          items: spec.items,
        })
      },
      closePalette: () => setOpen(false),
    }),
    [currentTheme, spaces],
  )

  // Recompute commandsItems whenever ctx changes so onSelect closures bind
  // the latest theme / spaces snapshot.
  const commandsItems = useMemo<CommandItem[]>(
    () => materializeCommands(ctx),
    [ctx],
  )

  const recentsItems = useMemo<CommandItem[]>(
    () =>
      recents.map((r) => ({
        id: `recent:${r.pageId}`,
        title: r.title || 'Untitled',
        icon: <Clock aria-hidden width={14} height={14} />,
        onSelect: () => navigateToPage(r.spaceId, r.pageId),
      })),
    [recents],
  )

  const searchItems = useMemo<CommandItem[]>(() => {
    const results: SearchResult[] | undefined = searchQuery.data
    if (!results) return []
    return results.map((r) => ({
      id: `page:${r.page_id}`,
      title: r.title || 'Untitled',
      subtitle: <HighlightedSnippet snippet={r.snippet} />,
      breadcrumb: r.breadcrumb.length > 0 ? r.breadcrumb.join(' / ') : undefined,
      icon: <FileText aria-hidden width={14} height={14} />,
      onSelect: () => navigateToPage(r.space_id, r.page_id),
    }))
  }, [searchQuery.data])

  const pagesItems = trimmedQuery.length === 0 ? recentsItems : searchItems

  const pagesEmptyMessage = useMemo<string | undefined>(() => {
    if (trimmedQuery.length === 0) {
      return recents.length > 0
        ? undefined
        : 'Recently viewed pages will appear here. Type to search.'
    }
    // Loading state — only visible on the very first query for a key, since
    // keepPreviousData keeps stale data around through subsequent re-queries.
    if (searchQuery.isFetching && !searchQuery.data) return 'Searching…'
    return `No pages match "${debouncedQuery}".`
  }, [
    trimmedQuery,
    debouncedQuery,
    recents.length,
    searchQuery.isFetching,
    searchQuery.data,
  ])

  return (
    <>
      <CommandPalette
        open={open}
        onOpenChange={handleOpenChange}
        initialMode={initialMode}
        pagesItems={pagesItems}
        commandsItems={commandsItems}
        subPicker={subPicker}
        searchRequest={searchRequest ?? undefined}
        pagesEmptyMessage={pagesEmptyMessage}
        onPagesQueryChange={setPagesQuery}
      />
      <NewPageDialog
        open={newPageOpen}
        onOpenChange={setNewPageOpen}
        defaultSpaceId={newPageDefaults.spaceId}
        defaultParentId={newPageDefaults.parentId}
      />
    </>
  )
}
