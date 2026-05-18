import { useCallback, useEffect, useMemo, useState } from 'react'
import { Clock } from 'lucide-react'
import {
  CommandPalette,
  prefixForMode,
  usePaletteShortcuts,
  type CommandItem,
  type CommandItemGroup,
  type PagesItems,
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
import { readRecentPages, type RecentPage } from '../../lib/recentPages'
import { useTier1TitleHits } from '../../lib/useTier1TitleHits'
import { useTier2SearchResults } from '../../lib/useTier2SearchResults'
import { useCommandPaletteState } from '../../lib/useCommandPaletteState'
import { pageHitToCommandItem, navigateToPage } from '../../lib/pageHitItem'

const RECENTS_VISIBLE = 8

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

// App-level palette mount. Orchestrates:
//  - palette UI state + keyboard contract (via useCommandPaletteState +
//    usePaletteShortcuts)
//  - tier-1 client title hits (via useTier1TitleHits) and tier-2 server
//    search (via useTier2SearchResults), then dedupes tier-2 against tier-1
//  - recently-viewed snapshot for the empty-state list
//  - CommandContext + command-registry materialization
//  - M4.2 new-page dialog bridge (Cmd-N + sidebar "+ New page" + command)
//
// Sits outside RouterProvider in App.tsx (sibling to it), so navigation goes
// through the imported `router` instance rather than useNavigate. Lives inside
// QueryClientProvider so useSpaces() / useQuery resolve from the shared cache.
export function AppCommandHost() {
  const {
    open,
    initialMode,
    subPicker,
    searchRequest,
    trimmedQuery,
    debouncedQuery,
    setPagesQuery,
    handleOpenChange,
    openWith,
    setSubPicker,
    pushSearchRequest,
    close,
  } = useCommandPaletteState()

  const titleHits = useTier1TitleHits(trimmedQuery, open)
  const searchResults = useTier2SearchResults(trimmedQuery)

  // Snapshot recently-viewed pages each time the palette opens so a navigation
  // away and back surfaces the freshly-visited page without remounting the
  // palette. Window 'storage' events would catch cross-tab writes but those
  // aren't a real scenario for single-user v0.
  const [recents, setRecents] = useState<RecentPage[]>([])
  useEffect(() => {
    // Snapshot recents on open — correct-by-design effect-driven setState
    // (memory.md "set-state-in-effect snapshot-on-open pattern").
    // eslint-disable-next-line react-hooks/set-state-in-effect
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
        // Switch the open palette into help mode without closing it.
        setSubPicker(null)
        pushSearchRequest(prefixForMode('help'))
      },
      openSubPicker: (spec: SubPickerSpec) => {
        setSubPicker({
          label: spec.label,
          placeholder: spec.placeholder,
          emptyMessage: spec.emptyMessage,
          items: spec.items,
        })
      },
      closePalette: close,
    }),
    [currentTheme, spaces, setSubPicker, pushSearchRequest, close],
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

  const titleItems = useMemo<CommandItem[]>(
    () => titleHits.map((h) => pageHitToCommandItem(h, { idPrefix: 'page-t1' })),
    [titleHits],
  )

  const searchItems = useMemo<CommandItem[]>(() => {
    if (!searchResults) return []
    // Drop server hits whose page already appears in tier-1 — tier-1 wins,
    // since a title-match is a stronger signal than a body-snippet match for
    // the common "find me the page by name" flow.
    const tier1Ids = new Set(titleHits.map((h) => h.pageId))
    return searchResults
      .filter((r) => !tier1Ids.has(r.page_id))
      .map((r) =>
        pageHitToCommandItem(
          {
            pageId: r.page_id,
            spaceId: r.space_id,
            title: r.title,
            breadcrumb: r.breadcrumb,
          },
          { idPrefix: 'page-t2', snippet: r.snippet },
        ),
      )
  }, [searchResults, titleHits])

  const pagesItems = useMemo<PagesItems>(() => {
    if (trimmedQuery.length === 0) return recentsItems
    // Grouped form: tier-1 renders under "Titles" at the top, tier-2 under
    // "All pages" below. Either group may be empty; we filter them out so the
    // surviving group renders unlabelled-ish (the heading still draws unless
    // the group is dropped entirely).
    const groups: CommandItemGroup[] = []
    if (titleItems.length > 0) {
      groups.push({ label: 'Titles', items: titleItems })
    }
    if (searchItems.length > 0) {
      groups.push({ label: 'All pages', items: searchItems })
    }
    return groups
  }, [trimmedQuery, recentsItems, titleItems, searchItems])

  const pagesEmptyMessage = useMemo<string | undefined>(() => {
    if (trimmedQuery.length === 0) {
      return recents.length > 0
        ? undefined
        : 'Recently viewed pages will appear here. Type to search.'
    }
    // If tier-1 has results we don't show empty messaging even while tier-2 is
    // still in flight — the user sees something instantly.
    if (titleItems.length > 0) return undefined
    // Loading state — only visible on the very first query for a key, since
    // keepPreviousData keeps stale data around through subsequent re-queries.
    if (searchResults == null) return 'Searching…'
    return `No pages match "${debouncedQuery}".`
  }, [
    trimmedQuery,
    debouncedQuery,
    recents.length,
    searchResults,
    titleItems.length,
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
