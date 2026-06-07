import { useState } from 'react'
import { Building2, Globe, History, KeyRound, MoreHorizontal, Trash2, UserPlus, Users } from 'lucide-react'
import { ApiError } from '../../lib/api'
import { useOrgAudit } from '../../lib/queries/org-audit'
import { AuditRow } from './SettingsAuditTab'
import {
  useAddOrgMember,
  useCreateOrg,
  useDeleteOrg,
  useOrgMembers,
  useOrgs,
  useRemoveOrgMember,
  useUpdateOrgMember,
} from '../../lib/queries/orgs'
import {
  useAddGroupMember,
  useCreateGroup,
  useDeleteGroup,
  useGroupMembers,
  useOrgGroups,
  useRemoveGroupMember,
} from '../../lib/queries/groups'
import {
  useCreateOrgDomain,
  useDeleteOrgDomain,
  useOrgDomains,
} from '../../lib/queries/org-domains'
import {
  useDeleteOrgSSO,
  useOrgSSO,
  usePutOrgSSO,
} from '../../lib/queries/org-sso'
import type { Group, Org, OrgMember, OrgRole } from '../../lib/types'
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'
import { Input } from '../ui/input'
import { Select } from '../ui/select'
import { Checkbox } from '../ui/checkbox'
import { cn } from '../../lib/utils'

// scope === 'instance' is the instance-admin view (every org, create/delete,
// domain mapping). scope === 'admin' is the org-admin self-service view: only
// the orgs the caller administers, members + groups + audit, but no
// create/delete-org and no domain mapping (those stay instance-admin only).
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
              ? 'Organizations group people so a whole team can be granted access to a space at once. Add members here, then share spaces with an org from its Share dialog.'
              : 'Organizations you administer. Manage members and groups, and review recent access changes. Creating organizations and mapping email domains is handled by an instance admin.'}
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
  const [manageOpen, setManageOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)

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
          <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium font-[family-name:var(--font-sans)]">
            {org.name}
          </span>
          <Badge variant="muted">
            {org.member_count} {org.member_count === 1 ? 'member' : 'members'}
          </Badge>
        </div>
        <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-mono)]">
          {org.slug}
        </span>
      </div>

      <Button type="button" variant="secondary" size="sm" onClick={() => setManageOpen(true)}>
        Manage members
      </Button>
      {scope === 'instance' ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              aria-label={`Actions for ${org.name}`}
              className="h-[var(--space-7)] w-[var(--space-7)] p-0"
            >
              <MoreHorizontal width={14} height={14} />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem destructive onSelect={() => setDeleteOpen(true)}>
              Delete org
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      ) : null}

      <ManageOrgDialog org={org} scope={scope} open={manageOpen} onOpenChange={setManageOpen} />
      {scope === 'instance' ? (
        <DeleteOrgDialog org={org} open={deleteOpen} onOpenChange={setDeleteOpen} />
      ) : null}
    </li>
  )
}

function ManageOrgDialog({
  org,
  scope,
  open,
  onOpenChange,
}: {
  org: Org
  scope: 'instance' | 'admin'
  open: boolean
  onOpenChange: (next: boolean) => void
}) {
  const members = useOrgMembers(open ? org.id : null)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Members of "{org.name}"</DialogTitle>
          <DialogDescription>
            Admins manage members and settings. Members just belong — sharing a
            space with this org gives all of them access.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-[var(--space-3)]">
          {members.isLoading ? (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
              Loading members…
            </p>
          ) : members.isError ? (
            <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
              Couldn't load members.
            </p>
          ) : members.data && members.data.length > 0 ? (
            <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
              {members.data.map((member) => (
                <OrgMemberRow key={member.user_id} orgId={org.id} member={member} />
              ))}
            </ul>
          ) : (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
              No members yet.
            </p>
          )}

          <AddOrgMemberForm orgId={org.id} />

          <GroupsSection org={org} />

          {scope === 'instance' ? (
            <OrgSSOSection orgId={open ? org.id : null} />
          ) : null}

          <OrgActivitySection orgId={open ? org.id : null} />
        </div>

        <DialogFooter>
          <DialogClose asChild>
            <Button type="button" variant="ghost">
              Close
            </Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// OrgSSOSection configures a single OIDC connection for the org (instance-admin
// only). The client_secret is write-only: it's never returned by the API, so it
// must be re-entered on each save. enforced=on refuses password login for the
// org's auto-join domains, funnelling those users through SSO.
function OrgSSOSection({ orgId }: { orgId: number | null }) {
  const sso = useOrgSSO(orgId)
  const put = usePutOrgSSO(orgId ?? 0)
  const del = useDeleteOrgSSO(orgId ?? 0)

  const [issuer, setIssuer] = useState('')
  const [clientId, setClientId] = useState('')
  const [clientSecret, setClientSecret] = useState('')
  const [enforced, setEnforced] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)
  // Prefill issuer/client_id/enforced from the loaded connection once.
  const [hydratedFor, setHydratedFor] = useState<number | null>(null)
  if (sso.data && hydratedFor !== orgId) {
    setIssuer(sso.data.issuer)
    setClientId(sso.data.client_id)
    setEnforced(sso.data.enforced)
    setHydratedFor(orgId)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (!issuer.trim() || !clientId.trim() || !clientSecret.trim()) {
      setError('Issuer, client ID and client secret are all required.')
      return
    }
    setError(null)
    setSaved(false)
    try {
      await put.mutateAsync({
        issuer: issuer.trim(),
        client_id: clientId.trim(),
        client_secret: clientSecret.trim(),
        enforced,
      })
      setClientSecret('')
      setSaved(true)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'issuer_unreachable') {
        setError("Couldn't run OIDC discovery against that issuer URL.")
      } else if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Failed to save SSO connection.')
      }
    }
  }

  async function handleRemove() {
    setError(null)
    try {
      await del.mutateAsync()
      setIssuer('')
      setClientId('')
      setClientSecret('')
      setEnforced(false)
      setHydratedFor(orgId)
    } catch {
      setError('Failed to remove SSO connection.')
    }
  }

  return (
    <section className="flex flex-col gap-[var(--space-3)] pt-[var(--space-3)] border-t border-[var(--border-subtle)]">
      <header className="flex flex-col gap-[var(--space-1)]">
        <h3 className="m-0 flex items-center gap-[var(--space-2)] font-[family-name:var(--font-sans)] text-[length:var(--text-base)] text-[var(--text-primary)]">
          <KeyRound width={15} height={15} />
          Single sign-on (OIDC)
          {sso.data?.configured ? <Badge variant="muted">configured</Badge> : null}
        </h3>
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
          Members sign in via this org's identity provider. Users land via the
          org's auto-join domains, so map at least one domain above. Enforcing
          SSO blocks password login for those domains.
        </p>
      </header>

      <form
        onSubmit={handleSubmit}
        noValidate
        className="flex flex-col gap-[var(--space-2)]"
      >
        <Input
          aria-label="Issuer URL"
          placeholder="https://idp.example.com"
          value={issuer}
          onChange={(e) => setIssuer(e.target.value)}
        />
        <Input
          aria-label="Client ID"
          placeholder="Client ID"
          value={clientId}
          onChange={(e) => setClientId(e.target.value)}
        />
        <Input
          type="password"
          aria-label="Client secret"
          autoComplete="off"
          placeholder={
            sso.data?.configured ? 'Client secret (re-enter to save)' : 'Client secret'
          }
          value={clientSecret}
          onChange={(e) => setClientSecret(e.target.value)}
        />
        <label className="flex items-center gap-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-primary)]">
          <Checkbox
            checked={enforced}
            onCheckedChange={(v) => setEnforced(v === true)}
          />
          Require SSO (block password login for this org's domains)
        </label>

        {error ? (
          <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
            {error}
          </p>
        ) : saved ? (
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
            Saved.
          </p>
        ) : null}

        <div className="flex items-center gap-[var(--space-2)]">
          <Button type="submit" variant="secondary" size="sm" disabled={put.isPending}>
            {put.isPending ? 'Saving…' : 'Save connection'}
          </Button>
          {sso.data?.configured ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => void handleRemove()}
              disabled={del.isPending}
              className="text-[var(--text-muted)] hover:text-[var(--danger)]"
            >
              Remove
            </Button>
          ) : null}
        </div>
      </form>
    </section>
  )
}

function OrgMemberRow({ orgId, member }: { orgId: number; member: OrgMember }) {
  const [rowError, setRowError] = useState<string | null>(null)
  const updateMember = useUpdateOrgMember()
  const removeMember = useRemoveOrgMember()

  async function handleRoleChange(org_role: OrgRole) {
    if (org_role === member.org_role) return
    setRowError(null)
    try {
      await updateMember.mutateAsync({ orgId, userId: member.user_id, org_role })
    } catch (err) {
      setRowError(orgMemberErrorMessage(err))
    }
  }

  async function handleRemove() {
    setRowError(null)
    try {
      await removeMember.mutateAsync({ orgId, userId: member.user_id })
    } catch (err) {
      setRowError(orgMemberErrorMessage(err))
    }
  }

  return (
    <li
      className={cn(
        'm-0 list-none flex items-center gap-[var(--space-3)]',
        'px-[var(--space-3)] py-[var(--space-2)]',
        'rounded-[var(--radius-sm)]',
        'border border-[var(--border-subtle)] bg-[var(--surface-1)]',
      )}
    >
      <div className="flex-1 min-w-0 flex flex-col gap-[2px]">
        <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium font-[family-name:var(--font-sans)]">
          {member.username}
        </span>
        {member.email ? (
          <span className="truncate text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
            {member.email}
          </span>
        ) : null}
        {rowError ? (
          <span role="alert" className="text-[length:var(--text-xs)] text-[var(--danger)]">
            {rowError}
          </span>
        ) : null}
      </div>

      <div className="w-[6.5rem] shrink-0">
        <Select
          size="sm"
          aria-label={`Role for ${member.username}`}
          value={member.org_role}
          disabled={updateMember.isPending}
          onChange={(e) => void handleRoleChange(e.target.value as OrgRole)}
        >
          <option value="admin">Admin</option>
          <option value="member">Member</option>
        </Select>
      </div>

      <Button
        type="button"
        variant="ghost"
        size="sm"
        aria-label={`Remove ${member.username}`}
        onClick={() => void handleRemove()}
        disabled={removeMember.isPending}
        className="text-[var(--text-muted)] hover:text-[var(--danger)]"
      >
        <Trash2 width={14} height={14} />
      </Button>
    </li>
  )
}

function AddOrgMemberForm({ orgId }: { orgId: number }) {
  const [identifier, setIdentifier] = useState('')
  const [role, setRole] = useState<OrgRole>('member')
  const [error, setError] = useState<string | null>(null)
  const addMember = useAddOrgMember()

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = identifier.trim()
    if (!trimmed) {
      setError('Email or username is required.')
      return
    }
    setError(null)
    try {
      await addMember.mutateAsync({ orgId, identifier: trimmed, org_role: role })
      setIdentifier('')
      setRole('member')
    } catch (err) {
      setError(addOrgMemberErrorMessage(err))
    }
  }

  return (
    <form
      onSubmit={handleSubmit}
      noValidate
      className={cn(
        'flex flex-col gap-[var(--space-2)]',
        'pt-[var(--space-3)]',
        'border-t border-[var(--border-subtle)]',
      )}
    >
      <label
        htmlFor={`add-org-member-${orgId}`}
        className="text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
      >
        Add a member
      </label>
      <div className="flex items-start gap-[var(--space-2)]">
        <div className="flex-1 min-w-0">
          <Input
            id={`add-org-member-${orgId}`}
            placeholder="Email or username"
            autoComplete="off"
            value={identifier}
            onChange={(e) => setIdentifier(e.target.value)}
            aria-invalid={error != null}
          />
        </div>
        <div className="w-[6.5rem] shrink-0">
          <Select
            size="md"
            aria-label="Role for new member"
            value={role}
            onChange={(e) => setRole(e.target.value as OrgRole)}
          >
            <option value="admin">Admin</option>
            <option value="member">Member</option>
          </Select>
        </div>
        <Button
          type="submit"
          variant="secondary"
          disabled={addMember.isPending || identifier.trim() === ''}
        >
          <UserPlus width={14} height={14} />
          <span>{addMember.isPending ? 'Adding…' : 'Add'}</span>
        </Button>
      </div>
      {error ? (
        <p role="alert" className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]">
          {error}
        </p>
      ) : null}
    </form>
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

function DeleteOrgDialog({
  org,
  open,
  onOpenChange,
}: {
  org: Org
  open: boolean
  onOpenChange: (next: boolean) => void
}) {
  const [error, setError] = useState<string | null>(null)
  const deleteOrg = useDeleteOrg()

  function handleClose(next: boolean) {
    if (!next) setError(null)
    onOpenChange(next)
  }

  async function handleConfirm() {
    setError(null)
    try {
      await deleteOrg.mutateAsync(org.id)
      handleClose(false)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete org.')
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete "{org.name}"?</DialogTitle>
          <DialogDescription>
            Members lose any space access they had through this org. Spaces and
            direct members are untouched. This can't be undone.
          </DialogDescription>
        </DialogHeader>
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
          <Button
            type="button"
            variant="danger"
            onClick={() => void handleConfirm()}
            disabled={deleteOrg.isPending}
          >
            {deleteOrg.isPending ? 'Deleting…' : 'Delete org'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Groups (sub-teams) within an org. Lives inside the org-management dialog,
// below members. Org admins create/delete groups and manage their members.
function GroupsSection({ org }: { org: Org }) {
  const groups = useOrgGroups(org.id)
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const createGroup = useCreateGroup()

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) {
      setError('Group name is required.')
      return
    }
    setError(null)
    try {
      await createGroup.mutateAsync({ orgId: org.id, name: trimmed })
      setName('')
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setError('A group with that name already exists.')
      } else if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Failed to create group.')
      }
    }
  }

  return (
    <div className="flex flex-col gap-[var(--space-2)] pt-[var(--space-3)] border-t border-[var(--border-subtle)]">
      <span className="flex items-center gap-[var(--space-2)] text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
        <Users width={12} height={12} />
        Groups
      </span>

      {groups.data && groups.data.length > 0 ? (
        <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
          {groups.data.map((g) => (
            <GroupRow key={g.id} org={org} group={g} />
          ))}
        </ul>
      ) : (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          No groups yet. Groups let you share a space with part of the org.
        </p>
      )}

      <form onSubmit={handleSubmit} noValidate className="flex items-start gap-[var(--space-2)]">
        <div className="flex-1 min-w-0">
          <Input
            placeholder="New group name (e.g. Engineering)"
            autoComplete="off"
            value={name}
            onChange={(e) => setName(e.target.value)}
            aria-invalid={error != null}
          />
        </div>
        <Button type="submit" variant="secondary" disabled={createGroup.isPending || name.trim() === ''}>
          <Users width={14} height={14} />
          <span>{createGroup.isPending ? 'Adding…' : 'Add group'}</span>
        </Button>
      </form>
      {error ? (
        <p role="alert" className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]">
          {error}
        </p>
      ) : null}
    </div>
  )
}

function GroupRow({ org, group }: { org: Org; group: Group }) {
  const [membersOpen, setMembersOpen] = useState(false)
  const deleteGroup = useDeleteGroup()

  return (
    <li
      className={cn(
        'm-0 list-none flex items-center gap-[var(--space-3)]',
        'px-[var(--space-3)] py-[var(--space-2)]',
        'rounded-[var(--radius-sm)]',
        'border border-[var(--border-subtle)] bg-[var(--surface-1)]',
      )}
    >
      <div className="flex-1 min-w-0 flex items-center gap-[var(--space-2)] flex-wrap">
        <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium font-[family-name:var(--font-sans)]">
          {group.name}
        </span>
        <Badge variant="muted">
          {group.member_count} {group.member_count === 1 ? 'member' : 'members'}
        </Badge>
      </div>
      <Button type="button" variant="ghost" size="sm" onClick={() => setMembersOpen(true)}>
        Members
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        aria-label={`Delete ${group.name}`}
        onClick={() => void deleteGroup.mutateAsync({ orgId: org.id, groupId: group.id })}
        disabled={deleteGroup.isPending}
        className="text-[var(--text-muted)] hover:text-[var(--danger)]"
      >
        <Trash2 width={14} height={14} />
      </Button>
      <GroupMembersDialog
        org={org}
        group={group}
        open={membersOpen}
        onOpenChange={setMembersOpen}
      />
    </li>
  )
}

function GroupMembersDialog({
  org,
  group,
  open,
  onOpenChange,
}: {
  org: Org
  group: Group
  open: boolean
  onOpenChange: (next: boolean) => void
}) {
  const members = useGroupMembers(open ? org.id : null, open ? group.id : null)
  const [identifier, setIdentifier] = useState('')
  const [error, setError] = useState<string | null>(null)
  const addMember = useAddGroupMember()
  const removeMember = useRemoveGroupMember()

  async function handleAdd(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = identifier.trim()
    if (!trimmed) {
      setError('Email or username is required.')
      return
    }
    setError(null)
    try {
      await addMember.mutateAsync({ orgId: org.id, groupId: group.id, identifier: trimmed })
      setIdentifier('')
    } catch (err) {
      if (err instanceof ApiError && err.code === 'not_org_member') {
        setError('Add them to the org first — group members must be org members.')
      } else if (err instanceof ApiError && err.status === 404) {
        setError('User not found.')
      } else if (err instanceof ApiError && err.status === 409) {
        setError('Already in this group.')
      } else if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Failed to add member.')
      }
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>"{group.name}" members</DialogTitle>
          <DialogDescription>
            Group members must already belong to {org.name}. Sharing a space with
            this group gives all of them access.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-[var(--space-3)]">
          {members.isLoading ? (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
              Loading members…
            </p>
          ) : members.data && members.data.length > 0 ? (
            <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
              {members.data.map((m) => (
                <li
                  key={m.user_id}
                  className={cn(
                    'm-0 list-none flex items-center gap-[var(--space-3)]',
                    'px-[var(--space-3)] py-[var(--space-2)]',
                    'rounded-[var(--radius-sm)]',
                    'border border-[var(--border-subtle)] bg-[var(--surface-1)]',
                  )}
                >
                  <div className="flex-1 min-w-0 flex flex-col gap-[1px]">
                    <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium font-[family-name:var(--font-sans)]">
                      {m.username}
                    </span>
                    {m.email ? (
                      <span className="truncate text-[length:var(--text-xs)] text-[var(--text-muted)]">
                        {m.email}
                      </span>
                    ) : null}
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    aria-label={`Remove ${m.username}`}
                    onClick={() =>
                      void removeMember.mutateAsync({
                        orgId: org.id,
                        groupId: group.id,
                        userId: m.user_id,
                      })
                    }
                    disabled={removeMember.isPending}
                    className="text-[var(--text-muted)] hover:text-[var(--danger)]"
                  >
                    <Trash2 width={14} height={14} />
                  </Button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
              No members yet.
            </p>
          )}

          <form
            onSubmit={handleAdd}
            noValidate
            className="flex items-start gap-[var(--space-2)] pt-[var(--space-3)] border-t border-[var(--border-subtle)]"
          >
            <div className="flex-1 min-w-0">
              <Input
                placeholder="Email or username (must be an org member)"
                autoComplete="off"
                value={identifier}
                onChange={(e) => setIdentifier(e.target.value)}
                aria-invalid={error != null}
              />
            </div>
            <Button type="submit" variant="secondary" disabled={addMember.isPending || identifier.trim() === ''}>
              <UserPlus width={14} height={14} />
              <span>{addMember.isPending ? 'Adding…' : 'Add'}</span>
            </Button>
          </form>
          {error ? (
            <p role="alert" className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]">
              {error}
            </p>
          ) : null}
        </div>

        <DialogFooter>
          <DialogClose asChild>
            <Button type="button" variant="ghost">
              Close
            </Button>
          </DialogClose>
        </DialogFooter>
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
          can't be removed without removing the mapping. Only domains you add
          here auto-join; avoid public providers like gmail.com.
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

// Org-scoped access history — who added whom, what was shared, which domains
// were mapped — for THIS org only. Backed by the org-admin-gated
// /api/orgs/{id}/audit; reuses AuditRow from the instance-wide audit tab.
function OrgActivitySection({ orgId }: { orgId: number | null }) {
  const audit = useOrgAudit(orgId)
  return (
    <div className="flex flex-col gap-[var(--space-2)] pt-[var(--space-3)] border-t border-[var(--border-subtle)]">
      <span className="flex items-center gap-[var(--space-2)] text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
        <History width={12} height={12} />
        Recent activity
      </span>
      {audit.isLoading ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          Loading activity…
        </p>
      ) : audit.isError ? (
        <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
          Couldn't load activity.
        </p>
      ) : audit.data && audit.data.length > 0 ? (
        <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
          {audit.data.map((e) => (
            <AuditRow key={e.id} entry={e} />
          ))}
        </ul>
      ) : (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          No changes recorded yet.
        </p>
      )}
    </div>
  )
}

function orgMemberErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === 'last_admin') {
      return "Can't remove the last admin — promote someone else first."
    }
    return err.message
  }
  return 'Something went wrong. Try again.'
}

function addOrgMemberErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 404) return 'User not found.'
    if (err.status === 409) return 'Already a member.'
    return err.message
  }
  return 'Failed to add member.'
}
