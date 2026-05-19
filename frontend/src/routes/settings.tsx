import { useMemo, useState } from 'react'
import { SettingsProfileTab } from '../components/app/SettingsProfileTab'
import { SettingsUsersTab } from '../components/app/SettingsUsersTab'
import { Button } from '../components/ui/button'
import { useMe } from '../lib/queries/auth'
import { cn } from '../lib/utils'

interface SettingsTab {
  id: string
  label: string
  render: () => React.ReactNode
}

const PROFILE_TAB: SettingsTab = {
  id: 'profile',
  label: 'Profile',
  render: () => <SettingsProfileTab />,
}

const USERS_TAB: SettingsTab = {
  id: 'users',
  label: 'Users',
  render: () => <SettingsUsersTab />,
}

export function SettingsPage() {
  const me = useMe()
  // The Users tab is gated on instance-admin; the array itself drops it for
  // non-admins so /settings looks identical to today's Profile-only shell.
  const tabs = useMemo<SettingsTab[]>(() => {
    if (me.data?.is_instance_admin) return [PROFILE_TAB, USERS_TAB]
    return [PROFILE_TAB]
  }, [me.data?.is_instance_admin])
  const [activeId, setActiveId] = useState(tabs[0].id)
  const active = tabs.find((t) => t.id === activeId) ?? tabs[0]

  return (
    <div className="flex-1 flex min-h-0">
      <nav
        aria-label="Settings sections"
        className="shrink-0 w-[var(--space-8)] sm:w-[14rem] border-r border-[var(--border-subtle)] bg-[var(--surface-2)] py-[var(--space-4)] px-[var(--space-3)] flex flex-col gap-[var(--space-1)]"
      >
        {tabs.map((tab) => {
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
