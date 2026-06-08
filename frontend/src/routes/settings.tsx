import { useMemo, useState } from 'react'
import { ImportSection } from '../components/app/ImportSection'
import { MiraImportSection } from '../components/app/MiraImportSection'
import { SettingsApiKeysTab } from '../components/app/SettingsApiKeysTab'
import { SettingsBillingTab } from '../components/app/SettingsBillingTab'
import { SettingsAuditTab } from '../components/app/SettingsAuditTab'
import { SettingsNotificationsTab } from '../components/app/SettingsNotificationsTab'
import { SettingsOrgsTab } from '../components/app/SettingsOrgsTab'
import { SettingsProfileTab } from '../components/app/SettingsProfileTab'
import { SettingsSearchIndexTab } from '../components/app/SettingsSearchIndexTab'
import { SettingsSyncTab } from '../components/app/SettingsSyncTab'
import { SettingsUsersTab } from '../components/app/SettingsUsersTab'
import { Button } from '../components/ui/button'
import { useMe } from '../lib/queries/auth'
import { useOrgs } from '../lib/queries/orgs'
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

const NOTIFICATIONS_TAB: SettingsTab = {
  id: 'notifications',
  label: 'Notifications',
  render: () => <SettingsNotificationsTab />,
}

const IMPORT_TAB: SettingsTab = {
  id: 'import',
  label: 'Import',
  render: () => (
    <>
      <ImportSection />
      <hr className="border-0 border-t border-[var(--border-subtle)]" />
      <MiraImportSection />
    </>
  ),
}

const API_KEYS_TAB: SettingsTab = {
  id: 'api-keys',
  label: 'API Keys',
  render: () => <SettingsApiKeysTab />,
}

const USERS_TAB: SettingsTab = {
  id: 'users',
  label: 'Users',
  render: () => <SettingsUsersTab />,
}

const ORGS_TAB: SettingsTab = {
  id: 'orgs',
  label: 'Organizations',
  render: () => <SettingsOrgsTab scope="instance" />,
}

// The org-admin self-service variant — shown to non-instance-admins who
// administer at least one org. Scoped to their orgs; no create/delete/domains.
const ORG_ADMIN_TAB: SettingsTab = {
  id: 'orgs',
  label: 'Organizations',
  render: () => <SettingsOrgsTab scope="admin" />,
}

const AUDIT_TAB: SettingsTab = {
  id: 'audit',
  label: 'Audit',
  render: () => <SettingsAuditTab />,
}

// Search index freshness — available to all users (scoped to their own spaces).
const SEARCH_INDEX_TAB: SettingsTab = {
  id: 'search-index',
  label: 'Search index',
  render: () => <SettingsSearchIndexTab />,
}

// "Connect your vault" — user self-service WebDAV sync, available to everyone
// (the backend gates token scope on the user's own space membership).
const SYNC_TAB: SettingsTab = {
  id: 'sync',
  label: 'Sync',
  render: () => <SettingsSyncTab />,
}

// Plan & usage — every account (personal + each org) carries a tier; available
// to all users.
const BILLING_TAB: SettingsTab = {
  id: 'billing',
  label: 'Plan & Usage',
  render: () => <SettingsBillingTab />,
}

export function SettingsPage() {
  const me = useMe()
  const orgs = useOrgs()
  // The Users + API Keys tabs are gated on instance-admin; the array itself
  // drops them for non-admins so /settings looks identical to today's
  // Profile-only shell. The backend gates /api/api_keys on instance-admin
  // too — mounting the tab for non-admins would just render a perpetual 403.
  // A non-instance-admin who administers an org gets a scoped Organizations
  // tab (member/group management + audit for their own orgs).
  const isOrgAdmin =
    !me.data?.is_instance_admin &&
    (orgs.data?.some((o) => o.my_role === 'admin') ?? false)
  const tabs = useMemo<SettingsTab[]>(() => {
    if (me.data?.is_instance_admin) {
      return [PROFILE_TAB, NOTIFICATIONS_TAB, BILLING_TAB, IMPORT_TAB, SEARCH_INDEX_TAB, SYNC_TAB, API_KEYS_TAB, USERS_TAB, ORGS_TAB, AUDIT_TAB]
    }
    if (isOrgAdmin) {
      return [PROFILE_TAB, NOTIFICATIONS_TAB, BILLING_TAB, IMPORT_TAB, SEARCH_INDEX_TAB, SYNC_TAB, ORG_ADMIN_TAB]
    }
    return [PROFILE_TAB, NOTIFICATIONS_TAB, BILLING_TAB, IMPORT_TAB, SEARCH_INDEX_TAB, SYNC_TAB]
  }, [me.data?.is_instance_admin, isOrgAdmin])
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
