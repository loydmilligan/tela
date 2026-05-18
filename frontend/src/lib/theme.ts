export type ThemeName = 'light' | 'dark' | 'warm'

export const THEMES: readonly ThemeName[] = ['light', 'dark', 'warm'] as const

const STORAGE_KEY = 'tela.theme'
const DEFAULT_THEME: ThemeName = 'light'

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
