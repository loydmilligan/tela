import { useSyncExternalStore } from 'react'

// Per-device UI behavior preferences — sidebar/editor niceties that are a
// property of THIS browser, not the account (so they live in localStorage, like
// theme.ts, not the server prefs used for notifications). Read synchronously,
// changed via setUiPref, observed with useUiPrefs (a useSyncExternalStore hook)
// so any component re-renders when a toggle flips from the settings screen.

export interface UiPrefs {
  /** #27 — a page created via "New child page" opens straight in edit mode. */
  newChildEditMode: boolean
  /** #28 — clicking a sidebar item that has children also expands its subtree. */
  clickExpandsChildren: boolean
}

const DEFAULTS: UiPrefs = {
  newChildEditMode: false,
  clickExpandsChildren: false,
}

const STORAGE_KEY = 'tela.ui-prefs'
const CHANGE_EVENT = 'tela:ui-prefs-change'

function read(): UiPrefs {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULTS
    const parsed = JSON.parse(raw) as Partial<UiPrefs>
    // Merge over defaults so an added pref key is absent-safe on old storage.
    return { ...DEFAULTS, ...parsed }
  } catch {
    return DEFAULTS
  }
}

export function getUiPrefs(): UiPrefs {
  if (typeof localStorage === 'undefined') return DEFAULTS
  return read()
}

export function setUiPref<K extends keyof UiPrefs>(key: K, value: UiPrefs[K]): void {
  const next = { ...read(), [key]: value }
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(next))
  } catch {
    // localStorage unavailable (private mode, etc.) — non-fatal; the event still
    // fires so live UI updates for this session.
  }
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent(CHANGE_EVENT))
  }
}

// useSyncExternalStore needs a stable snapshot reference or it loops. read()
// builds a fresh object each call, so cache it and only replace when the stored
// JSON actually changes.
let cache: UiPrefs = DEFAULTS
let cacheKey = '\0init'

function snapshot(): UiPrefs {
  if (typeof localStorage === 'undefined') return DEFAULTS
  let raw: string | null
  try {
    raw = localStorage.getItem(STORAGE_KEY)
  } catch {
    raw = null
  }
  const key = raw ?? '\0null'
  if (key !== cacheKey) {
    cache = read()
    cacheKey = key
  }
  return cache
}

function subscribe(cb: () => void): () => void {
  if (typeof window === 'undefined') return () => {}
  window.addEventListener(CHANGE_EVENT, cb)
  // Cross-tab: a change in another tab fires `storage`, not our CustomEvent.
  window.addEventListener('storage', cb)
  return () => {
    window.removeEventListener(CHANGE_EVENT, cb)
    window.removeEventListener('storage', cb)
  }
}

export function useUiPrefs(): UiPrefs {
  return useSyncExternalStore(subscribe, snapshot, () => DEFAULTS)
}
