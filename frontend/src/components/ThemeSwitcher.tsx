import { useState } from 'react'
import { getTheme, setTheme, THEMES, type ThemeName } from '../lib/theme'

export function ThemeSwitcher() {
  const [active, setActive] = useState<ThemeName>(() => getTheme())

  const handleSelect = (name: ThemeName) => {
    setTheme(name)
    setActive(name)
  }

  return (
    <div
      role="group"
      aria-label="Theme"
      style={{
        display: 'inline-flex',
        gap: 'var(--space-2)',
        padding: 'var(--space-1)',
        background: 'var(--surface-2)',
        border: '1px solid var(--border-subtle)',
        borderRadius: 'var(--radius-md)',
      }}
    >
      {THEMES.map((name) => {
        const isActive = active === name
        return (
          <button
            key={name}
            type="button"
            onClick={() => handleSelect(name)}
            aria-pressed={isActive}
            style={{
              padding: 'var(--space-2) var(--space-4)',
              fontFamily: 'var(--font-sans)',
              fontSize: 'var(--text-sm)',
              lineHeight: 'var(--leading-tight)',
              borderRadius: 'var(--radius-sm)',
              border: '1px solid transparent',
              cursor: 'pointer',
              background: isActive ? 'var(--accent)' : 'transparent',
              color: isActive ? 'var(--accent-fg)' : 'var(--text-primary)',
              textTransform: 'capitalize',
              transition:
                'background-color var(--duration-fast) var(--ease-out), color var(--duration-fast) var(--ease-out)',
            }}
          >
            {name}
          </button>
        )
      })}
    </div>
  )
}
