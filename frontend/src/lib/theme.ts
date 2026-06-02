export type ThemeName = 'light' | 'dark' | 'warm'

export const THEMES: readonly ThemeName[] = ['light', 'dark', 'warm'] as const

const STORAGE_KEY = 'tela.theme'
const DEFAULT_THEME: ThemeName = 'light'
const THEME_CHANGE_EVENT = 'tela:theme-change'

function isThemeName(value: string | null): value is ThemeName {
  return value !== null && (THEMES as readonly string[]).includes(value)
}

export function getTheme(): ThemeName {
  if (typeof document === 'undefined') return DEFAULT_THEME
  const attr = document.documentElement.getAttribute('data-theme')
  return isThemeName(attr) ? attr : DEFAULT_THEME
}

export function setTheme(name: ThemeName): void {
  document.documentElement.setAttribute('data-theme', name)
  try {
    localStorage.setItem(STORAGE_KEY, name)
  } catch {
    // localStorage may be unavailable (private mode, etc.) — non-fatal.
  }
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent<ThemeName>(THEME_CHANGE_EVENT, { detail: name }))
  }
}

// Subscribe to theme changes triggered via setTheme (from any caller —
// ThemeSwitcher, the toggle-theme command, future settings UI). Lets sibling
// UI re-sync without lifting theme state to a React context.
export function subscribeToTheme(cb: (next: ThemeName) => void): () => void {
  if (typeof window === 'undefined') return () => {}
  function handler(e: Event) {
    cb((e as CustomEvent<ThemeName>).detail)
  }
  window.addEventListener(THEME_CHANGE_EVENT, handler)
  return () => window.removeEventListener(THEME_CHANGE_EVENT, handler)
}

// #3 PDF export — apply a theme passed via ?theme= on the reader URL gotenberg
// loads (/print, /share). Sets data-theme so the chosen palette renders, plus a
// data-pdf-themed marker that opts the page out of the print-CSS forced-light
// default. No-op (keeps the viewer's own theme) when the param is absent/invalid,
// so it's safe to call on the human-facing share view too. Returns the applied
// theme, or null when nothing was applied.
export function applyPdfThemeParam(): ThemeName | null {
  if (typeof window === 'undefined') return null
  try {
    const value = new URLSearchParams(window.location.search).get('theme')
    if (!isThemeName(value)) return null
    document.documentElement.setAttribute('data-theme', value)
    document.documentElement.setAttribute('data-pdf-themed', '')
    return value
  } catch {
    return null
  }
}

export function initTheme(): void {
  let stored: string | null
  try {
    stored = localStorage.getItem(STORAGE_KEY)
  } catch {
    stored = null
  }
  const theme = isThemeName(stored) ? stored : DEFAULT_THEME
  document.documentElement.setAttribute('data-theme', theme)
}
