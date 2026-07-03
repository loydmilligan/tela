import { useEffect, useRef } from 'react'
import {
  getKeyBindings,
  keysOf,
  type KeyBinding,
  type KeyContext,
} from './keymap'
import {
  activeRegion,
  currentSurface,
  isNormalMode,
  listActivate,
  listEdge,
  listMove,
  readerEdge,
  readerScroll,
  readerSection,
} from './regions'

// How long a leader key (`g`) waits for its second keystroke before resetting.
const LEADER_TIMEOUT = 800

// Host-provided action verbs — wired to palette events / router / theme by
// KeymapHost. The engine supplies the motion verbs and `surface` itself.
export type KeymapActions = Pick<
  KeyContext,
  | 'navigate'
  | 'openPalette'
  | 'openNewPage'
  | 'toggleTheme'
  | 'toggleSidebar'
  | 'openCheatsheet'
>

// Build the motion half of the context against whatever region is live now.
// No region → motion verbs are silent no-ops (e.g. `j` on a bare page view
// with no sidebar tree).
function motionContext(): Pick<
  KeyContext,
  | 'down'
  | 'up'
  | 'top'
  | 'bottom'
  | 'activate'
  | 'prevSection'
  | 'nextSection'
> {
  const region = activeRegion()
  if (!region) {
    const noop = () => {}
    return {
      down: noop,
      up: noop,
      top: noop,
      bottom: noop,
      activate: noop,
      prevSection: noop,
      nextSection: noop,
    }
  }
  const { el, kind } = region
  if (kind === 'reader') {
    return {
      down: () => readerScroll(el, 1),
      up: () => readerScroll(el, -1),
      top: () => readerEdge(el, 'top'),
      bottom: () => readerEdge(el, 'bottom'),
      activate: () => {},
      prevSection: () => readerSection(el, -1),
      nextSection: () => readerSection(el, 1),
    }
  }
  return {
    down: () => listMove(el, 1),
    up: () => listMove(el, -1),
    top: () => listEdge(el, 'first'),
    bottom: () => listEdge(el, 'last'),
    activate: () => listActivate(el),
    prevSection: () => {},
    nextSection: () => {},
  }
}

// Resolve a typed token (single key or `pending + ' ' + key`) to a binding
// whose surface gate passes.
function match(token: string, surface: 'app' | 'public'): KeyBinding | null {
  for (const b of getKeyBindings()) {
    if (b.when && b.when !== surface) continue
    if (keysOf(b).includes(token)) return b
  }
  return null
}

// True when some surface-applicable binding starts with `token + ' '` — i.e.
// `token` is a leader prefix we should wait on rather than fire immediately.
function isLeader(token: string, surface: 'app' | 'public'): boolean {
  const prefix = token + ' '
  for (const b of getKeyBindings()) {
    if (b.when && b.when !== surface) continue
    if (keysOf(b).some((k) => k.startsWith(prefix))) return true
  }
  return false
}

// Window-level bare-key + leader-sequence dispatcher. Modifier combos
// (Cmd/Ctrl) are intentionally left to useGlobalShortcut / the palette — this
// layer ignores them so the two don't double-fire. Fires only in normal mode
// (see isNormalMode).
export function useKeymap(actions: KeymapActions): void {
  const actionsRef = useRef(actions)
  useEffect(() => {
    actionsRef.current = actions
  })

  useEffect(() => {
    let pending: string | null = null
    let timer: number | undefined

    const reset = () => {
      pending = null
      if (timer) window.clearTimeout(timer)
      timer = undefined
    }

    function run(binding: KeyBinding, e: KeyboardEvent) {
      e.preventDefault()
      binding.run({
        ...actionsRef.current,
        ...motionContext(),
        surface: currentSurface(),
      })
    }

    function onKeyDown(e: KeyboardEvent) {
      // Leave modifier combos to the palette/global-shortcut layer.
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (!isNormalMode(e)) {
        if (pending) reset()
        return
      }
      const surface = currentSurface()
      const key = e.key

      // Mid-sequence: only the full `pending key` combo can match. Either way
      // the leader is consumed (no fall-through to a fresh single key).
      if (pending) {
        const combo = `${pending} ${key}`
        reset()
        const binding = match(combo, surface)
        if (binding) run(binding, e)
        return
      }

      // Fresh key that opens a sequence → buffer and wait for the next key.
      if (isLeader(key, surface)) {
        pending = key
        timer = window.setTimeout(reset, LEADER_TIMEOUT)
        e.preventDefault()
        return
      }

      const binding = match(key, surface)
      if (binding) run(binding, e)
    }

    window.addEventListener('keydown', onKeyDown)
    return () => {
      reset()
      window.removeEventListener('keydown', onKeyDown)
    }
  }, [])
}
