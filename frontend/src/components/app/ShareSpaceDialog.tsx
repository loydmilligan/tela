import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Trash2, UserPlus } from 'lucide-react'
import { ApiError } from '../../lib/api'
import { useMe } from '../../lib/queries/auth'
import {
  useAddSpaceMember,
  useRemoveSpaceMember,
  useSpaceMembers,
  useUpdateSpaceMember,
} from '../../lib/queries/members'
import type { Space, SpaceMember } from '../../lib/types'
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

const ROLE_LABEL: Record<SpaceMember['role'], string> = {
  owner: 'Owner',
  editor: 'Editor',
  viewer: 'Viewer',
}

interface ShareSpaceDialogProps {
  space: Space
  open: boolean
  onOpenChange: (next: boolean) => void
}

export function ShareSpaceDialog({
  space,
  open,
  onOpenChange,
}: ShareSpaceDialogProps) {
  const me = useMe()
  const members = useSpaceMembers(open ? space.id : null)

  const myMembership =
    me.data != null
      ? (members.data?.find((m) => m.user_id === me.data!.id) ?? null)
      : null
  const iAmOwner = myMembership?.role === 'owner'

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Share "{space.name}"</DialogTitle>
          <DialogDescription>
            Members of this space can view its pages. Owners can change roles
            and invite new members.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-[var(--space-3)]">
          {members.isLoading ? (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
              Loading members…
            </p>
          ) : members.isError ? (
            <p
              role="alert"
              className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
            >
              Couldn't load members.
            </p>
          ) : members.data && members.data.length > 0 ? (
            <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-1)]">
              {members.data.map((member) => (
                <MemberRow
                  key={member.user_id}
                  space={space}
                  member={member}
                  isSelf={me.data?.id === member.user_id}
                  iAmOwner={iAmOwner}
                  onSelfLeave={() => onOpenChange(false)}
                />
              ))}
            </ul>
          ) : null}

          {iAmOwner ? (
            <AddMemberForm spaceId={space.id} />
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

interface MemberRowProps {
  space: Space
  member: SpaceMember
  isSelf: boolean
  iAmOwner: boolean
  onSelfLeave: () => void
}

function MemberRow({
  space,
  member,
  isSelf,
  iAmOwner,
  onSelfLeave,
}: MemberRowProps) {
  const [rowError, setRowError] = useState<string | null>(null)
  const [leaveOpen, setLeaveOpen] = useState(false)
  const updateMember = useUpdateSpaceMember()
  const removeMember = useRemoveSpaceMember()

  const canEditRole = iAmOwner && !isSelf

  async function handleRoleChange(role: SpaceMember['role']) {
    if (role === member.role) return
    setRowError(null)
    try {
      await updateMember.mutateAsync({
        spaceId: space.id,
        userId: member.user_id,
        role,
      })
    } catch (err) {
      setRowError(memberErrorMessage(err))
    }
  }

  async function handleRemove() {
    setRowError(null)
    try {
      await removeMember.mutateAsync({
        spaceId: space.id,
        userId: member.user_id,
      })
    } catch (err) {
      setRowError(memberErrorMessage(err))
    }
  }

  return (
    <li
      className={cn(
        'm-0 list-none',
        'flex items-center gap-[var(--space-3)]',
        'px-[var(--space-3)] py-[var(--space-2)]',
        'rounded-[var(--radius-sm)]',
        'border border-[var(--border-subtle)] bg-[var(--surface-1)]',
      )}
    >
      <div className="flex-1 min-w-0 flex flex-col gap-[2px]">
        <div className="flex items-center gap-[var(--space-2)] min-w-0 flex-wrap">
          <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium font-[family-name:var(--font-sans)]">
            {member.username}
          </span>
          {isSelf ? <Badge variant="muted">You</Badge> : null}
        </div>
        {rowError ? (
          <span
            role="alert"
            className="text-[length:var(--text-xs)] text-[var(--danger)]"
          >
            {rowError}
          </span>
        ) : null}
      </div>

      {canEditRole ? (
        <div className="w-[6.5rem] shrink-0">
          <Select
            size="sm"
            aria-label={`Role for ${member.username}`}
            value={member.role}
            disabled={updateMember.isPending}
            onChange={(e) =>
              void handleRoleChange(e.target.value as SpaceMember['role'])
            }
          >
            <option value="owner">Owner</option>
            <option value="editor">Editor</option>
            <option value="viewer">Viewer</option>
          </Select>
        </div>
      ) : (
        <Badge variant={member.role === 'owner' ? 'accent' : 'muted'}>
          {ROLE_LABEL[member.role]}
        </Badge>
      )}

      {iAmOwner && !isSelf ? (
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
      ) : null}

      {!iAmOwner && isSelf ? (
        <>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => setLeaveOpen(true)}
          >
            Leave space
          </Button>
          <LeaveSpaceConfirmDialog
            space={space}
            open={leaveOpen}
            onOpenChange={setLeaveOpen}
            onLeft={onSelfLeave}
          />
        </>
      ) : null}
    </li>
  )
}

interface LeaveSpaceConfirmDialogProps {
  space: Space
  open: boolean
  onOpenChange: (next: boolean) => void
  onLeft: () => void
}

function LeaveSpaceConfirmDialog({
  space,
  open,
  onOpenChange,
  onLeft,
}: LeaveSpaceConfirmDialogProps) {
  const me = useMe()
  const navigate = useNavigate()
  const removeMember = useRemoveSpaceMember()
  const [error, setError] = useState<string | null>(null)

  function handleClose(next: boolean) {
    if (!next) setError(null)
    onOpenChange(next)
  }

  async function handleConfirm() {
    if (me.data == null) return
    setError(null)
    try {
      await removeMember.mutateAsync({
        spaceId: space.id,
        userId: me.data.id,
        isSelf: true,
      })
      handleClose(false)
      onLeft()
      void navigate({ to: '/' })
    } catch (err) {
      setError(memberErrorMessage(err))
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Leave "{space.name}"?</DialogTitle>
          <DialogDescription>
            You'll lose access to this space and its pages. An owner will need
            to re-invite you to come back.
          </DialogDescription>
        </DialogHeader>
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
          <Button
            type="button"
            variant="danger"
            onClick={() => void handleConfirm()}
            disabled={removeMember.isPending}
          >
            {removeMember.isPending ? 'Leaving…' : 'Leave space'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function AddMemberForm({ spaceId }: { spaceId: number }) {
  const [username, setUsername] = useState('')
  const [role, setRole] = useState<SpaceMember['role']>('viewer')
  const [error, setError] = useState<string | null>(null)
  const addMember = useAddSpaceMember()

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = username.trim()
    if (!trimmed) {
      setError('Username is required.')
      return
    }
    setError(null)
    try {
      await addMember.mutateAsync({ spaceId, username: trimmed, role })
      setUsername('')
      setRole('viewer')
    } catch (err) {
      setError(addMemberErrorMessage(err))
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
        htmlFor={`add-member-username-${spaceId}`}
        className="text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
      >
        Add a member
      </label>
      <div className="flex items-start gap-[var(--space-2)]">
        <div className="flex-1 min-w-0">
          <Input
            id={`add-member-username-${spaceId}`}
            placeholder="Username"
            autoComplete="off"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            aria-invalid={error != null}
          />
        </div>
        <div className="w-[6.5rem] shrink-0">
          <Select
            size="md"
            aria-label="Role for new member"
            value={role}
            onChange={(e) => setRole(e.target.value as SpaceMember['role'])}
          >
            <option value="owner">Owner</option>
            <option value="editor">Editor</option>
            <option value="viewer">Viewer</option>
          </Select>
        </div>
        <Button
          type="submit"
          variant="secondary"
          disabled={addMember.isPending || username.trim() === ''}
        >
          <UserPlus width={14} height={14} />
          <span>{addMember.isPending ? 'Adding…' : 'Add'}</span>
        </Button>
      </div>
      {error ? (
        <p
          role="alert"
          className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]"
        >
          {error}
        </p>
      ) : null}
    </form>
  )
}

function memberErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === 'last_owner') {
      return "Can't remove the last owner — promote someone else first."
    }
    return err.message
  }
  return 'Something went wrong. Try again.'
}

function addMemberErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 404) return 'User not found.'
    if (err.status === 409) return 'Already a member.'
    if (err.code === 'bad_request') return err.message
    return err.message
  }
  return 'Failed to add member.'
}
