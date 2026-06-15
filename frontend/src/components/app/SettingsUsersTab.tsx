import { useMemo, useState } from 'react'
import { Boxes, MoreHorizontal, Plug, Search, Sparkles, UserPlus } from 'lucide-react'
import { ApiError } from '../../lib/api'
import { useMe } from '../../lib/queries/auth'
import {
  useAdminUsers,
  useAdminUserActivity,
  useCreateAdminUser,
  useUpdateAdminUser,
} from '../../lib/queries/admin-users'
import { navigateToPage } from '../../lib/pageHitItem'
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '../ui/sheet'
import {
  localDateFromSqlite,
  relativeTimeFromSqlite,
} from '../../lib/relativeTime'
import { formatBytes } from '../../lib/format'
import type { AdminUserRow, AdminUserUsage } from '../../lib/types'
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'
import { Input } from '../ui/input'
import { cn } from '../../lib/utils'

const MIN_PASSWORD_LEN = 8

type RoleFilter = 'all' | 'admin'
type StatusFilter = 'all' | 'active' | 'inactive'
type McpFilter = 'all' | 'yes'

export function SettingsUsersTab() {
  const me = useMe()
  const users = useAdminUsers()
  const [createOpen, setCreateOpen] = useState(false)
  const [q, setQ] = useState('')
  const [role, setRole] = useState<RoleFilter>('all')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [mcp, setMcp] = useState<McpFilter>('all')

  const all = useMemo(() => users.data ?? [], [users.data])
  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return all.filter((u) => {
      if (role === 'admin' && !u.is_instance_admin) return false
      if (status === 'active' && !u.is_active) return false
      if (status === 'inactive' && u.is_active) return false
      if (mcp === 'yes' && !(u.used_mcp || u.has_api_key)) return false
      if (needle) {
        const hay = `${u.username} ${u.display_name ?? ''} ${u.email ?? ''}`.toLowerCase()
        if (!hay.includes(needle)) return false
      }
      return true
    })
  }, [all, q, role, status, mcp])

  const hasFilters = q.trim() !== '' || role !== 'all' || status !== 'all' || mcp !== 'all'

  return (
    <section
      aria-labelledby="settings-users"
      className="flex flex-col gap-[var(--space-4)]"
    >
      <header className="flex items-start justify-between gap-[var(--space-3)]">
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
          Manage every account on this instance. Reset passwords, deactivate
          sign-ins, grant instance-admin, or see who's connected an agent (MCP).
        </p>
        <Button
          type="button"
          variant="primary"
          onClick={() => setCreateOpen(true)}
        >
          <UserPlus width={14} height={14} />
          <span>Create user</span>
        </Button>
      </header>

      {/* Filter bar */}
      <div className="flex flex-wrap items-center gap-[var(--space-2)]">
        <div className="relative flex-1 min-w-[12rem]">
          <Search
            width={14}
            height={14}
            aria-hidden
            className="pointer-events-none absolute left-[var(--space-2)] top-1/2 -translate-y-1/2 text-[var(--text-muted)]"
          />
          <Input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search name or email…"
            aria-label="Search users"
            className="pl-[var(--space-6)]"
          />
        </div>
        <FilterPills<RoleFilter>
          value={role}
          onChange={setRole}
          options={[
            ['all', 'All'],
            ['admin', 'Admins'],
          ]}
        />
        <FilterPills<StatusFilter>
          value={status}
          onChange={setStatus}
          options={[
            ['all', 'Any status'],
            ['active', 'Active'],
            ['inactive', 'Inactive'],
          ]}
        />
        <FilterPills<McpFilter>
          value={mcp}
          onChange={setMcp}
          options={[
            ['all', 'Any MCP'],
            ['yes', 'MCP set up'],
          ]}
        />
      </div>

      {users.isLoading ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          Loading users…
        </p>
      ) : users.isError ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
          Couldn't load users.
        </p>
      ) : filtered.length > 0 ? (
        <>
          <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)] tabular-nums">
            {filtered.length} of {all.length} {all.length === 1 ? 'user' : 'users'}
          </p>
          <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
            {filtered.map((u) => (
              <UserRow key={u.id} row={u} isSelf={me.data?.id === u.id} />
            ))}
          </ul>
        </>
      ) : (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          {hasFilters ? 'No users match these filters.' : 'No users found.'}
        </p>
      )}

      <CreateUserDialog open={createOpen} onOpenChange={setCreateOpen} />
    </section>
  )
}

// A small segmented control of mutually-exclusive filter values.
function FilterPills<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T
  onChange: (next: T) => void
  options: [T, string][]
}) {
  return (
    <div className="inline-flex items-center gap-[1px] rounded-[var(--radius-sm)] border border-[var(--border-subtle)] p-[2px]">
      {options.map(([val, label]) => (
        <button
          key={val}
          type="button"
          onClick={() => onChange(val)}
          aria-pressed={value === val}
          className={cn(
            'rounded-[var(--radius-xs)] px-[var(--space-2)] py-[2px] text-[length:var(--text-xs)] font-medium',
            'outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]',
            value === val
              ? 'bg-[var(--surface-3)] text-[var(--text-primary)]'
              : 'text-[var(--text-muted)] hover:text-[var(--text-primary)]',
          )}
        >
          {label}
        </button>
      ))}
    </div>
  )
}

function UserRow({ row, isSelf }: { row: AdminUserRow; isSelf: boolean }) {
  const [resetOpen, setResetOpen] = useState(false)
  const [activityOpen, setActivityOpen] = useState(false)
  const [rowError, setRowError] = useState<string | null>(null)
  const updateUser = useUpdateAdminUser()

  async function runUpdate(input: {
    is_active?: boolean
    is_instance_admin?: boolean
  }) {
    setRowError(null)
    try {
      await updateUser.mutateAsync({ id: row.id, ...input })
    } catch (err) {
      if (err instanceof ApiError && err.code === 'last_admin') {
        setRowError(
          "Can't deactivate or demote the last instance admin — promote someone first.",
        )
      } else if (err instanceof ApiError) {
        setRowError(err.message)
      } else {
        setRowError('Something went wrong. Try again.')
      }
    }
  }

  return (
    <li
      className={cn(
        'm-0 list-none',
        'flex items-center gap-[var(--space-3)]',
        'px-[var(--space-3)] py-[var(--space-3)]',
        'rounded-[var(--radius-sm)]',
        'border border-[var(--border-subtle)] bg-[var(--surface-1)]',
      )}
    >
      <button
        type="button"
        onClick={() => setActivityOpen(true)}
        className="flex-1 min-w-0 flex flex-col gap-[2px] text-left bg-transparent border-0 cursor-pointer rounded-[var(--radius-xs)] outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
        aria-label={`Details for ${row.username}`}
      >
        <div className="flex items-center gap-[var(--space-2)] min-w-0 flex-wrap">
          <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium font-[family-name:var(--font-sans)]">
            {row.display_name || row.username}
          </span>
          {row.display_name ? (
            <span className="truncate text-[length:var(--text-xs)] text-[var(--text-muted)]">
              @{row.username}
            </span>
          ) : null}
          {row.is_instance_admin ? <Badge variant="accent">Instance admin</Badge> : null}
          {!row.is_active ? <Badge variant="muted">Deactivated</Badge> : null}
          {isSelf ? <Badge variant="muted">You</Badge> : null}
          <McpBadge row={row} />
        </div>
        <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
          {row.email ? `${row.email} · ` : ''}Created{' '}
          {localDateFromSqlite(row.created_at)}
        </span>
        {rowError ? (
          <span
            role="alert"
            className="text-[length:var(--text-xs)] text-[var(--danger)]"
          >
            {rowError}
          </span>
        ) : null}
      </button>
      <UsageCell row={row} />
      <PlanTierSelect
        accountKind="user"
        accountId={row.id}
        currentKey={row.plan_key}
        className="w-[9rem] shrink-0"
      />
      {isSelf ? null : (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              aria-label={`Actions for ${row.username}`}
              className="h-[var(--space-7)] w-[var(--space-7)] p-0"
              disabled={updateUser.isPending}
            >
              <MoreHorizontal width={14} height={14} />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={() => setActivityOpen(true)}>
              View activity
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => setResetOpen(true)}>
              Reset password
            </DropdownMenuItem>
            <DropdownMenuItem
              onSelect={() =>
                void runUpdate({ is_instance_admin: !row.is_instance_admin })
              }
            >
              {row.is_instance_admin
                ? 'Revoke instance admin'
                : 'Make instance admin'}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              destructive={row.is_active}
              onSelect={() => void runUpdate({ is_active: !row.is_active })}
            >
              {row.is_active ? 'Deactivate' : 'Reactivate'}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      <ResetPasswordDialog
        user={row}
        open={resetOpen}
        onOpenChange={setResetOpen}
      />
      <UserActivitySheet
        user={row}
        open={activityOpen}
        onOpenChange={setActivityOpen}
      />
    </li>
  )
}

// Instance-wide recent edits by one user — the latest edit per page, newest
// first. Reuses the recent-changes feed shape; querying is deferred until the
// drawer opens. Clicking a row jumps to that page (which leaves Settings).
function UserActivitySheet({
  user,
  open,
  onOpenChange,
}: {
  user: AdminUserRow
  open: boolean
  onOpenChange: (next: boolean) => void
}) {
  const activity = useAdminUserActivity(user.id, open)
  const u = user.usage
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-[min(28rem,100vw)]">
        <SheetHeader>
          <SheetTitle>{user.display_name || user.username}</SheetTitle>
          <SheetDescription>
            @{user.username}
            {user.email ? ` · ${user.email}${user.email_verified ? ' ✓' : ' (unverified)'}` : ''}
          </SheetDescription>
        </SheetHeader>
        <SheetBody>
          {/* Stat grid — everything the list row can't fit. */}
          <div className="mb-[var(--space-4)] grid grid-cols-2 gap-[var(--space-2)]">
            <DetailStat icon={<Boxes width={14} height={14} />} label="Spaces" value={u ? String(u.spaces) : '—'} />
            <DetailStat icon={<Boxes width={14} height={14} />} label="Orgs" value={String(user.orgs ?? 0)} />
            <DetailStat icon={<Sparkles width={14} height={14} />} label="AI calls / mo" value={String(user.llm_calls ?? 0)} />
            <DetailStat
              icon={<Plug width={14} height={14} />}
              label="MCP"
              value={user.used_mcp ? 'Connected' : user.has_api_key ? 'Key, no use' : 'Not set up'}
            />
            <DetailStat label="Storage" value={u ? formatBytes(u.storage_bytes) : '—'} />
            <DetailStat
              label="Last active"
              value={user.last_active_at ? relativeTimeFromSqlite(user.last_active_at) : 'Never'}
            />
          </div>
          <p className="m-0 mb-[var(--space-2)] text-[length:var(--text-xs)] font-medium uppercase tracking-wide text-[var(--text-muted)]">
            Recent edits
          </p>
          {activity.isLoading ? (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
              Loading…
            </p>
          ) : activity.isError ? (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
              Couldn't load activity.
            </p>
          ) : activity.data && activity.data.length > 0 ? (
            <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
              {activity.data.map((c) => (
                <li key={c.page_id}>
                  <button
                    type="button"
                    onClick={() => {
                      onOpenChange(false)
                      navigateToPage(c.space_id, c.page_id)
                    }}
                    className={cn(
                      'w-full text-left flex flex-col gap-[2px]',
                      'px-[var(--space-3)] py-[var(--space-2)]',
                      'rounded-[var(--radius-sm)] bg-transparent border-0 cursor-pointer',
                      'outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]',
                      'hover:bg-[var(--surface-2)]',
                    )}
                  >
                    <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-[family-name:var(--font-sans)]">
                      {c.title || 'Untitled'}
                    </span>
                    <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
                      {c.space_name} · {relativeTimeFromSqlite(c.updated_at)}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
              No edits yet.
            </p>
          )}
        </SheetBody>
      </SheetContent>
    </Sheet>
  )
}

// True when usage has crossed a finite plan limit (storage or spaces) — drives
// the danger styling so an over-quota account stands out at a glance.
function isOverLimit(u: AdminUserUsage): boolean {
  return (
    (u.max_storage_bytes != null && u.storage_bytes > u.max_storage_bytes) ||
    (u.max_spaces != null && u.spaces > u.max_spaces)
  )
}

// The MCP-setup signal: an accent "MCP" badge once the user has actually hit
// /api/mcp; a quieter "API key" mark if they have a PAT but no MCP traffic yet;
// nothing otherwise. (OAuth/cowork connections leave no DB trace, so a green
// badge can under-report — never over-report.)
function McpBadge({ row }: { row: AdminUserRow }) {
  if (row.used_mcp) return <Badge variant="accent">MCP</Badge>
  if (row.has_api_key) return <Badge variant="muted">API key</Badge>
  return null
}

// One labeled stat in the user detail sheet's grid.
function DetailStat({
  icon,
  label,
  value,
}: {
  icon?: React.ReactNode
  label: string
  value: string
}) {
  return (
    <div className="flex flex-col gap-[2px] rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--surface-1)] px-[var(--space-3)] py-[var(--space-2)]">
      <span className="inline-flex items-center gap-[var(--space-1)] text-[length:var(--text-xs)] text-[var(--text-muted)]">
        {icon}
        {label}
      </span>
      <span className="text-[length:var(--text-sm)] font-medium text-[var(--text-primary)]">
        {value}
      </span>
    </div>
  )
}

// Compact usage + last-active readout, right-aligned before the plan selector.
// Hidden on narrow widths where the row would otherwise wrap awkwardly.
function UsageCell({ row }: { row: AdminUserRow }) {
  const u = row.usage
  const orgs = row.orgs ?? 0
  const llm = row.llm_calls ?? 0
  return (
    <div className="hidden sm:flex flex-col items-end gap-[2px] shrink-0 w-[11rem] text-[length:var(--text-xs)] font-[family-name:var(--font-sans)]">
      {u ? (
        <span
          className={cn(
            'tabular-nums',
            isOverLimit(u) ? 'text-[var(--danger)] font-medium' : 'text-[var(--text-muted)]',
          )}
        >
          {u.spaces} {u.spaces === 1 ? 'space' : 'spaces'}
          {orgs > 0 ? ` · ${orgs} ${orgs === 1 ? 'org' : 'orgs'}` : ''}
          {llm > 0 ? ` · ${llm} AI` : ''}
        </span>
      ) : null}
      <span className="text-[var(--text-muted)]">
        {row.last_active_at
          ? `Active ${relativeTimeFromSqlite(row.last_active_at)}`
          : 'Never signed in'}
      </span>
    </div>
  )
}

function CreateUserDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (next: boolean) => void
}) {
  const [username, setUsername] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [makeAdmin, setMakeAdmin] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const createUser = useCreateAdminUser()

  function reset() {
    setUsername('')
    setEmail('')
    setPassword('')
    setMakeAdmin(false)
    setError(null)
  }

  function handleClose(next: boolean) {
    if (!next) reset()
    onOpenChange(next)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmedUsername = username.trim()
    if (!trimmedUsername) {
      setError('Username is required.')
      return
    }
    if (password.length < MIN_PASSWORD_LEN) {
      setError(`Password must be at least ${MIN_PASSWORD_LEN} characters.`)
      return
    }
    setError(null)
    const trimmedEmail = email.trim()
    try {
      await createUser.mutateAsync({
        username: trimmedUsername,
        ...(trimmedEmail ? { email: trimmedEmail } : {}),
        password,
        is_instance_admin: makeAdmin,
      })
      handleClose(false)
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        setError('That username or email is already taken.')
      } else if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Failed to create user.')
      }
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create a new user</DialogTitle>
          <DialogDescription>
            The user can change their password later from Settings → Profile.
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={handleSubmit}
          className="flex flex-col gap-[var(--space-3)]"
          noValidate
        >
          <div className="flex flex-col gap-[var(--space-2)]">
            <label
              htmlFor="new-user-username"
              className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
            >
              Username
            </label>
            <Input
              id="new-user-username"
              autoFocus
              autoComplete="off"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              aria-invalid={error != null}
            />
          </div>
          <div className="flex flex-col gap-[var(--space-2)]">
            <label
              htmlFor="new-user-email"
              className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
            >
              Email <span className="text-[var(--text-muted)]">(optional)</span>
            </label>
            <Input
              id="new-user-email"
              type="email"
              autoComplete="off"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              aria-invalid={error != null}
            />
          </div>
          <div className="flex flex-col gap-[var(--space-2)]">
            <label
              htmlFor="new-user-password"
              className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
            >
              Initial password
            </label>
            <Input
              id="new-user-password"
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              aria-invalid={error != null}
            />
          </div>
          <label className="flex items-center gap-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-primary)] cursor-pointer">
            <Checkbox
              checked={makeAdmin}
              onCheckedChange={(next) => setMakeAdmin(next === true)}
            />
            <span>Make this user an instance admin</span>
          </label>
          {error ? (
            <p
              role="alert"
              className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]"
            >
              {error}
            </p>
          ) : null}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="ghost">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={createUser.isPending}>
              {createUser.isPending ? 'Creating…' : 'Create user'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ResetPasswordDialog({
  user,
  open,
  onOpenChange,
}: {
  user: AdminUserRow
  open: boolean
  onOpenChange: (next: boolean) => void
}) {
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const updateUser = useUpdateAdminUser()

  function handleClose(next: boolean) {
    if (!next) {
      setPassword('')
      setError(null)
    }
    onOpenChange(next)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    if (password.length < MIN_PASSWORD_LEN) {
      setError(`Password must be at least ${MIN_PASSWORD_LEN} characters.`)
      return
    }
    setError(null)
    try {
      await updateUser.mutateAsync({ id: user.id, password })
      handleClose(false)
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Failed to reset password.')
      }
    }
  }

  // Stable form id keyed off the user so multiple ResetPasswordDialog
  // instances in the same list don't share an input id.
  const formId = useMemo(() => `reset-password-${user.id}`, [user.id])

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Reset password for {user.username}</DialogTitle>
          <DialogDescription>
            The user will be signed out of every device after this change.
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={handleSubmit}
          className="flex flex-col gap-[var(--space-3)]"
          noValidate
        >
          <div className="flex flex-col gap-[var(--space-2)]">
            <label
              htmlFor={formId}
              className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
            >
              New password
            </label>
            <Input
              id={formId}
              type="password"
              autoComplete="new-password"
              autoFocus
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              aria-invalid={error != null}
            />
          </div>
          {error ? (
            <p
              role="alert"
              className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]"
            >
              {error}
            </p>
          ) : null}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="ghost">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={updateUser.isPending}>
              {updateUser.isPending ? 'Saving…' : 'Reset password'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
