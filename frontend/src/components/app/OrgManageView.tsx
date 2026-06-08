import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import {
  ArrowLeft,
  Globe,
  History,
  KeyRound,
  Trash2,
  UserPlus,
  Users,
} from 'lucide-react'
import { ApiError } from '../../lib/api'
import { useMe } from '../../lib/queries/auth'
import { useOrgAudit } from '../../lib/queries/org-audit'
import { useOrgDomains } from '../../lib/queries/org-domains'
import {
  useAddOrgMember,
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
import { useDeleteOrgSSO, useOrgSSO, usePutOrgSSO } from '../../lib/queries/org-sso'
import type { Group, Org, OrgMember, OrgRole } from '../../lib/types'
import { AuditRow } from './SettingsAuditTab'
import { PlanTierSelect } from './PlanTierSelect'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { Checkbox } from '../ui/checkbox'
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../ui/tabs'
import { cn } from '../../lib/utils'

// Dedicated per-org management page (route /settings/orgs/$orgId). Replaces the
// old crowded "Members of X" dialog: the org's people, groups, single sign-on,
// and activity each get their own sub-tab with room to grow. Reached from the
// Organizations list in Settings. Instance admins see every org plus the SSO
// tab and the danger zone; org admins see only the orgs they administer and
// manage members/groups/activity.

export function OrgManageView({ orgId }: { orgId: number }) {
  const me = useMe()
  const orgs = useOrgs()
  const isInstance = me.data?.is_instance_admin ?? false
  const org = orgs.data?.find((o) => o.id === orgId)
  const canManage = isInstance || org?.my_role === 'admin'

  const backLink = (
    <Link
      to="/settings"
      search={{ tab: 'orgs' }}
      className="inline-flex items-center gap-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-muted)] hover:text-[var(--text-primary)] no-underline"
    >
      <ArrowLeft width={14} height={14} />
      Organizations
    </Link>
  )

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-[48rem] w-full mx-auto p-[var(--space-7)] flex flex-col gap-[var(--space-6)]">
        {backLink}

        {orgs.isLoading || me.isLoading ? (
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
            Loading organization…
          </p>
        ) : !org || !canManage ? (
          <div className="flex flex-col gap-[var(--space-2)]">
            <h1 className="m-0 font-[family-name:var(--font-sans)] text-[length:var(--text-2xl)] text-[var(--text-primary)]">
              {org ? 'No access' : 'Organization not found'}
            </h1>
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
              {org
                ? "You don't administer this organization."
                : "This organization doesn't exist or you can't see it."}
            </p>
          </div>
        ) : (
          <OrgManageBody org={org} isInstance={isInstance} />
        )}
      </div>
    </div>
  )
}

function OrgManageBody({ org, isInstance }: { org: Org; isInstance: boolean }) {
  return (
    <>
      <header className="flex flex-col gap-[var(--space-3)]">
        <div className="flex items-start justify-between gap-[var(--space-3)] flex-wrap">
          <div className="flex flex-col gap-[var(--space-1)] min-w-0">
            <div className="flex items-center gap-[var(--space-2)] flex-wrap">
              <h1 className="m-0 font-[family-name:var(--font-sans)] text-[length:var(--text-2xl)] leading-[var(--leading-tight)] text-[var(--text-primary)] truncate">
                {org.name}
              </h1>
              <Badge variant="muted">
                {org.member_count} {org.member_count === 1 ? 'member' : 'members'}
              </Badge>
            </div>
            <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-mono)]">
              {org.slug}
            </span>
          </div>
          {isInstance ? (
            <PlanTierSelect
              accountKind="org"
              accountId={org.id}
              currentKey={org.plan_key}
              className="w-[9rem] shrink-0"
            />
          ) : null}
        </div>
      </header>

      <Tabs defaultValue="members" className="flex flex-col gap-[var(--space-5)]">
        <TabsList>
          <TabsTrigger value="members">
            <Users width={14} height={14} />
            Members
          </TabsTrigger>
          <TabsTrigger value="groups">
            <Users width={14} height={14} />
            Groups
          </TabsTrigger>
          {isInstance ? (
            <TabsTrigger value="sso">
              <KeyRound width={14} height={14} />
              Single sign-on
            </TabsTrigger>
          ) : null}
          <TabsTrigger value="activity">
            <History width={14} height={14} />
            Activity
          </TabsTrigger>
        </TabsList>

        <TabsContent value="members">
          <MembersPanel org={org} />
        </TabsContent>
        <TabsContent value="groups">
          <GroupsPanel org={org} />
        </TabsContent>
        {isInstance ? (
          <TabsContent value="sso">
            <SSOPanel org={org} />
          </TabsContent>
        ) : null}
        <TabsContent value="activity">
          <OrgActivityPanel orgId={org.id} />
        </TabsContent>
      </Tabs>

      {isInstance ? <DangerZone org={org} /> : null}
    </>
  )
}

function MembersPanel({ org }: { org: Org }) {
  const members = useOrgMembers(org.id)
  return (
    <div className="flex flex-col gap-[var(--space-4)]">
      <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
        Admins manage members and settings. Members just belong — sharing a space
        with this org gives all of them access.
      </p>
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
    </div>
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
        'pt-[var(--space-4)]',
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

function GroupsPanel({ org }: { org: Org }) {
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
    <div className="flex flex-col gap-[var(--space-4)]">
      <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
        Groups are sub-teams within the org. Share a space with a group to give
        just part of the org access.
      </p>

      {groups.data && groups.data.length > 0 ? (
        <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
          {groups.data.map((g) => (
            <GroupRow key={g.id} org={org} group={g} />
          ))}
        </ul>
      ) : (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          No groups yet.
        </p>
      )}

      <form
        onSubmit={handleSubmit}
        noValidate
        className="flex items-start gap-[var(--space-2)] pt-[var(--space-4)] border-t border-[var(--border-subtle)]"
      >
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

// OrgSSOSection configures a single OIDC connection for the org (instance-admin
// only). The client_secret is write-only: it's never returned by the API, so it
// must be re-entered on each save. enforced=on refuses password login for the
// org's auto-join domains, funnelling those users through SSO.
function SSOPanel({ org }: { org: Org }) {
  const orgId = org.id
  const sso = useOrgSSO(orgId)
  const put = usePutOrgSSO(orgId)
  const del = useDeleteOrgSSO(orgId)
  const domains = useOrgDomains()
  const orgDomains = (domains.data ?? []).filter((d) => d.org_id === orgId)

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
    <div className="flex flex-col gap-[var(--space-4)]">
      <header className="flex flex-col gap-[var(--space-1)]">
        <div className="flex items-center gap-[var(--space-2)]">
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
            Members sign in via this org's OIDC identity provider.
          </p>
          {sso.data?.configured ? <Badge variant="muted">configured</Badge> : null}
        </div>
      </header>

      {/* Auto-join domains drive who lands on this connection. Read-only here —
          mapping is managed in Settings → Organizations. */}
      <div className="flex flex-col gap-[var(--space-2)] rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--surface-2)] p-[var(--space-3)]">
        <span className="flex items-center gap-[var(--space-2)] text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
          <Globe width={12} height={12} />
          Auto-join domains
        </span>
        {orgDomains.length > 0 ? (
          <div className="flex flex-wrap gap-[var(--space-2)]">
            {orgDomains.map((d) => (
              <span
                key={d.domain}
                className="text-[length:var(--text-sm)] text-[var(--text-primary)] font-[family-name:var(--font-mono)]"
              >
                {d.domain}
              </span>
            ))}
          </div>
        ) : (
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
            No domains mapped yet. Map at least one in{' '}
            <Link
              to="/settings"
              search={{ tab: 'orgs' }}
              className="text-[var(--accent)] no-underline hover:underline"
            >
              Organizations
            </Link>{' '}
            so members resolve to this connection.
          </p>
        )}
      </div>

      <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-[var(--space-2)]">
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
    </div>
  )
}

// Org-scoped access history — who added whom, what was shared, which domains
// were mapped — for THIS org only. Backed by the org-admin-gated
// /api/orgs/{id}/audit; reuses AuditRow from the instance-wide audit tab.
function OrgActivityPanel({ orgId }: { orgId: number }) {
  const audit = useOrgAudit(orgId)
  return (
    <div className="flex flex-col gap-[var(--space-3)]">
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

function DangerZone({ org }: { org: Org }) {
  const [open, setOpen] = useState(false)
  return (
    <section className="flex flex-col gap-[var(--space-3)] rounded-[var(--radius-md)] border border-[var(--border-strong)] p-[var(--space-4)]">
      <header className="flex flex-col gap-[var(--space-1)]">
        <h2 className="m-0 font-[family-name:var(--font-sans)] text-[length:var(--text-base)] text-[var(--text-primary)]">
          Danger zone
        </h2>
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
          Deleting an org removes the space access its members had through it.
          Spaces and direct members are untouched.
        </p>
      </header>
      <div>
        <Button type="button" variant="danger" size="sm" onClick={() => setOpen(true)}>
          <Trash2 width={14} height={14} />
          <span>Delete organization</span>
        </Button>
      </div>
      <DeleteOrgDialog org={org} open={open} onOpenChange={setOpen} />
    </section>
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
      // Land back on the org list; this org no longer exists.
      window.location.assign('/settings?tab=orgs')
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
    // 402 quota_exceeded — the org's seat limit. The backend message already
    // names the plan + cap; surface it verbatim.
    if (err.status === 402) return err.message
    return err.message
  }
  return 'Failed to add member.'
}
