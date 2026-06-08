import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { Building2, ChevronRight, Globe, Trash2 } from 'lucide-react'
import { ApiError } from '../../lib/api'
import { useCreateOrg, useOrgs } from '../../lib/queries/orgs'
import {
  useCreateOrgDomain,
  useDeleteOrgDomain,
  useOrgDomains,
} from '../../lib/queries/org-domains'
import type { Org } from '../../lib/types'
import { PlanTierSelect } from './PlanTierSelect'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog'
import { Input } from '../ui/input'
import { Select } from '../ui/select'
import { cn } from '../../lib/utils'

// The Organizations tab is now just an index: a list of orgs that each link to
// the dedicated per-org management page (route /settings/orgs/$orgId), plus org
// creation and the instance-wide auto-join domain map. Everything that used to
// live in the crowded "Members of X" dialog — members, groups, SSO, activity —
// moved to OrgManageView. scope === 'instance' is the instance-admin view
// (every org, create, domain mapping); scope === 'admin' is the org-admin
// self-service view (only the orgs they administer, no create/domains).
export function SettingsOrgsTab({
  scope = 'instance',
}: {
  scope?: 'instance' | 'admin'
}) {
  const orgs = useOrgs()
  const [createOpen, setCreateOpen] = useState(false)
  const isInstance = scope === 'instance'

  const visibleOrgs = isInstance
    ? orgs.data ?? []
    : (orgs.data ?? []).filter((o) => o.my_role === 'admin')

  return (
    <section
      aria-labelledby="settings-orgs"
      className="flex flex-col gap-[var(--space-6)]"
    >
      <div className="flex flex-col gap-[var(--space-4)]">
        <header className="flex items-start justify-between gap-[var(--space-3)]">
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
            {isInstance
              ? 'Organizations group people so a whole team can be granted access to a space at once. Open one to manage its members, groups, and single sign-on.'
              : 'Organizations you administer. Open one to manage its members and groups, and review recent access changes. Creating organizations and mapping email domains is handled by an instance admin.'}
          </p>
          {isInstance ? (
            <Button type="button" variant="primary" onClick={() => setCreateOpen(true)}>
              <Building2 width={14} height={14} />
              <span>New org</span>
            </Button>
          ) : null}
        </header>

        {orgs.isLoading ? (
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
            Loading organizations…
          </p>
        ) : orgs.isError ? (
          <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
            Couldn't load organizations.
          </p>
        ) : visibleOrgs.length > 0 ? (
          <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
            {visibleOrgs.map((org) => (
              <OrgRow key={org.id} org={org} scope={scope} />
            ))}
          </ul>
        ) : (
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
            {isInstance
              ? 'No organizations yet. Create one to start grouping members.'
              : 'You don’t administer any organizations.'}
          </p>
        )}
      </div>

      {isInstance ? (
        <>
          <hr className="border-0 border-t border-[var(--border-subtle)]" />
          <DomainsSection orgs={orgs.data ?? []} />
          <CreateOrgDialog open={createOpen} onOpenChange={setCreateOpen} />
        </>
      ) : null}
    </section>
  )
}

function OrgRow({ org, scope }: { org: Org; scope: 'instance' | 'admin' }) {
  return (
    <li
      className={cn(
        'm-0 list-none flex items-center gap-[var(--space-3)]',
        'px-[var(--space-3)] py-[var(--space-3)]',
        'rounded-[var(--radius-sm)]',
        'border border-[var(--border-subtle)] bg-[var(--surface-1)]',
      )}
    >
      <div className="flex-1 min-w-0 flex flex-col gap-[2px]">
        <div className="flex items-center gap-[var(--space-2)] min-w-0 flex-wrap">
          <Link
            to="/settings/orgs/$orgId"
            params={{ orgId: org.id }}
            className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium font-[family-name:var(--font-sans)] no-underline hover:underline"
          >
            {org.name}
          </Link>
          <Badge variant="muted">
            {org.member_count} {org.member_count === 1 ? 'member' : 'members'}
          </Badge>
        </div>
        <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-mono)]">
          {org.slug}
        </span>
      </div>

      {scope === 'instance' ? (
        <PlanTierSelect
          accountKind="org"
          accountId={org.id}
          currentKey={org.plan_key}
          className="w-[9rem] shrink-0"
        />
      ) : null}
      <Button asChild variant="secondary" size="sm">
        <Link to="/settings/orgs/$orgId" params={{ orgId: org.id }}>
          <span>Manage</span>
          <ChevronRight width={14} height={14} />
        </Link>
      </Button>
    </li>
  )
}

function CreateOrgDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (next: boolean) => void
}) {
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [error, setError] = useState<string | null>(null)
  const createOrg = useCreateOrg()

  function reset() {
    setName('')
    setSlug('')
    setError(null)
  }

  function handleClose(next: boolean) {
    if (!next) reset()
    onOpenChange(next)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmedName = name.trim()
    if (!trimmedName) {
      setError('Name is required.')
      return
    }
    setError(null)
    const trimmedSlug = slug.trim()
    try {
      await createOrg.mutateAsync({
        name: trimmedName,
        ...(trimmedSlug ? { slug: trimmedSlug } : {}),
      })
      handleClose(false)
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setError('An org with that slug already exists.')
      } else if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Failed to create org.')
      }
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create an organization</DialogTitle>
          <DialogDescription>
            You'll add members after it's created.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="flex flex-col gap-[var(--space-3)]" noValidate>
          <div className="flex flex-col gap-[var(--space-2)]">
            <label htmlFor="new-org-name" className="text-[length:var(--text-sm)] text-[var(--text-muted)]">
              Name
            </label>
            <Input
              id="new-org-name"
              autoFocus
              autoComplete="off"
              value={name}
              onChange={(e) => setName(e.target.value)}
              aria-invalid={error != null}
            />
          </div>
          <div className="flex flex-col gap-[var(--space-2)]">
            <label htmlFor="new-org-slug" className="text-[length:var(--text-sm)] text-[var(--text-muted)]">
              Slug <span className="text-[var(--text-muted)]">(optional — derived from name)</span>
            </label>
            <Input
              id="new-org-slug"
              autoComplete="off"
              placeholder="acme-inc"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              aria-invalid={error != null}
            />
          </div>
          {error ? (
            <p role="alert" className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]">
              {error}
            </p>
          ) : null}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="ghost">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={createOrg.isPending}>
              {createOrg.isPending ? 'Creating…' : 'Create org'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function DomainsSection({ orgs }: { orgs: Org[] }) {
  const domains = useOrgDomains()
  const [domain, setDomain] = useState('')
  const [orgId, setOrgId] = useState<number | ''>('')
  const [error, setError] = useState<string | null>(null)
  const createDomain = useCreateOrgDomain()
  const deleteDomain = useDeleteOrgDomain()

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = domain.trim()
    if (!trimmed) {
      setError('Domain is required.')
      return
    }
    if (orgId === '') {
      setError('Pick an org to map the domain to.')
      return
    }
    setError(null)
    try {
      await createDomain.mutateAsync({ domain: trimmed, org_id: orgId })
      setDomain('')
      setOrgId('')
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setError('That domain is already mapped to an org.')
      } else if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Failed to add domain.')
      }
    }
  }

  return (
    <div className="flex flex-col gap-[var(--space-4)]">
      <header className="flex flex-col gap-[var(--space-1)]">
        <h2 className="m-0 flex items-center gap-[var(--space-2)] font-[family-name:var(--font-sans)] text-[length:var(--text-lg)] text-[var(--text-primary)]">
          <Globe width={16} height={16} />
          Auto-join domains
        </h2>
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
          A user whose verified email domain matches is enrolled into the mapped
          org as a <strong className="font-medium text-[var(--text-primary)]">member</strong>{' '}
          automatically on sign-in — membership is identity-derived, so they
          can't be removed without removing the mapping. Domains here also route
          users to the org's single sign-on. Avoid public providers like
          gmail.com.
        </p>
      </header>

      {domains.data && domains.data.length > 0 ? (
        <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
          {domains.data.map((d) => (
            <li
              key={d.domain}
              className={cn(
                'm-0 list-none flex items-center gap-[var(--space-3)]',
                'px-[var(--space-3)] py-[var(--space-2)]',
                'rounded-[var(--radius-sm)]',
                'border border-[var(--border-subtle)] bg-[var(--surface-1)]',
              )}
            >
              <div className="flex-1 min-w-0 flex items-center gap-[var(--space-2)] flex-wrap">
                <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-[family-name:var(--font-mono)]">
                  {d.domain}
                </span>
                <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">→</span>
                <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)]">
                  {d.org_name}
                </span>
              </div>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                aria-label={`Remove ${d.domain}`}
                onClick={() => void deleteDomain.mutateAsync(d.domain)}
                disabled={deleteDomain.isPending}
                className="text-[var(--text-muted)] hover:text-[var(--danger)]"
              >
                <Trash2 width={14} height={14} />
              </Button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          No auto-join domains configured.
        </p>
      )}

      <form
        onSubmit={handleSubmit}
        noValidate
        className="flex flex-col gap-[var(--space-2)] pt-[var(--space-3)] border-t border-[var(--border-subtle)]"
      >
        <label
          htmlFor="add-org-domain"
          className="text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
        >
          Map a domain
        </label>
        <div className="flex items-start gap-[var(--space-2)] flex-wrap">
          <div className="flex-1 min-w-[10rem]">
            <Input
              id="add-org-domain"
              placeholder="acme.com"
              autoComplete="off"
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              aria-invalid={error != null}
            />
          </div>
          <div className="w-[10rem] shrink-0">
            <Select
              size="md"
              aria-label="Org to map the domain to"
              value={orgId === '' ? '' : String(orgId)}
              onChange={(e) => setOrgId(e.target.value === '' ? '' : Number(e.target.value))}
            >
              <option value="">Choose org…</option>
              {orgs.map((o) => (
                <option key={o.id} value={o.id}>
                  {o.name}
                </option>
              ))}
            </Select>
          </div>
          <Button type="submit" variant="secondary" disabled={createDomain.isPending}>
            <span>{createDomain.isPending ? 'Adding…' : 'Add'}</span>
          </Button>
        </div>
        {error ? (
          <p role="alert" className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]">
            {error}
          </p>
        ) : null}
      </form>
    </div>
  )
}
