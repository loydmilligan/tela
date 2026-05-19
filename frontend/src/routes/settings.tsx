import { useState } from 'react'
import { SettingsProfileTab } from '../components/app/SettingsProfileTab'
import { Button } from '../components/ui/button'
import { cn } from '../lib/utils'

interface SettingsTab {
  id: string
  label: string
  render: () => React.ReactNode
}

// Tabs the rail offers. Only 'Profile' lives here today; the rail is built
// from this list so M6.5 / M6.6 can drop additional entries (e.g. Users,
// Spaces) without restructuring the shell.
const TABS: SettingsTab[] = [
  {
    id: 'profile',
    label: 'Profile',
    render: () => <SettingsProfileTab />,
  },
]

export function SettingsPage() {
  const [activeId, setActiveId] = useState(TABS[0].id)
  const active = TABS.find((t) => t.id === activeId) ?? TABS[0]

  return (
    <div className="flex-1 flex min-h-0">
      <nav
        aria-label="Settings sections"
        className="shrink-0 w-[var(--space-8)] sm:w-[14rem] border-r border-[var(--border-subtle)] bg-[var(--surface-2)] py-[var(--space-4)] px-[var(--space-3)] flex flex-col gap-[var(--space-1)]"
      >
        {TABS.map((tab) => {
          const isActive = tab.id === active.id
          return (
            <Button
              key={tab.id}
              type="button"
              variant="ghost"
              size="sm"
              className={cn(
                'w-full justify-start',
                isActive &&
                  'bg-[var(--surface-3)] text-[var(--text-primary)] font-medium',
              )}
              aria-current={isActive ? 'page' : undefined}
              onClick={() => setActiveId(tab.id)}
            >
              {tab.label}
            </Button>
          )
        })}
      </nav>
      <div className="flex-1 overflow-y-auto">
        <div className="max-w-[48rem] w-full mx-auto p-[var(--space-7)] flex flex-col gap-[var(--space-6)]">
          <header className="flex flex-col gap-[var(--space-1)]">
            <h1 className="m-0 font-[family-name:var(--font-sans)] text-[length:var(--text-2xl)] leading-[var(--leading-tight)] text-[var(--text-primary)]">
              {active.label}
            </h1>
          </header>
          {active.render()}
        </div>
      </div>
    </div>
  )
}
