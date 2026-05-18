import { useCallback, useState } from 'react'
import type { CommandMode, CommandSubPicker } from '../components/ui/command'
import { useDebouncedValue } from './useDebouncedValue'

// Search debounce target. 50ms is fast enough that the user can't perceive a
// gap between stop-typing and result-arrival, but slow enough that rapid typing
// collapses into a single in-flight request rather than one per key.
const SEARCH_DEBOUNCE_MS = 50

export interface CommandPaletteState {
  open: boolean
  initialMode: CommandMode
  subPicker: CommandSubPicker | null
  searchRequest: { value: string; nonce: number } | null
  pagesQuery: string
  debouncedQuery: string
  trimmedQuery: string
  setPagesQuery: (q: string) => void
  handleOpenChange: (next: boolean) => void
  openWith: (mode: CommandMode) => void
  setSubPicker: (sp: CommandSubPicker | null) => void
  // Push a search value into the open palette (e.g., a command switching the
  // palette into help mode). Nonce is bumped here so consumers don't have to
  // think about it.
  pushSearchRequest: (value: string) => void
  close: () => void
}

// All transient command-palette UI state and the callbacks that mutate it.
// Extracted from AppCommandHost so future state additions (M5.2 wikilink
// picker bridge, etc.) don't keep growing the host.
export function useCommandPaletteState(): CommandPaletteState {
  const [open, setOpen] = useState(false)
  const [initialMode, setInitialMode] = useState<CommandMode>('pages')
  const [subPicker, setSubPicker] = useState<CommandSubPicker | null>(null)
  const [searchRequest, setSearchRequest] = useState<
    { value: string; nonce: number } | null
  >(null)

  // Pages-mode query: the palette pushes the current query in via
  // onPagesQueryChange; we debounce and expose trimmed for the search hooks.
  const [pagesQuery, setPagesQuery] = useState('')
  const debouncedQuery = useDebouncedValue(pagesQuery, SEARCH_DEBOUNCE_MS)
  const trimmedQuery = debouncedQuery.trim()

  // Clear transient state on close so the next open starts fresh — no stale
  // sub-picker, no leftover external search push, no zombie pages query
  // feeding a search round-trip.
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

  const pushSearchRequest = useCallback((value: string) => {
    setSearchRequest((prev) => ({ value, nonce: (prev?.nonce ?? 0) + 1 }))
  }, [])

  const close = useCallback(() => setOpen(false), [])

  return {
    open,
    initialMode,
    subPicker,
    searchRequest,
    pagesQuery,
    debouncedQuery,
    trimmedQuery,
    setPagesQuery,
    handleOpenChange,
    openWith,
    setSubPicker,
    pushSearchRequest,
    close,
  }
}
