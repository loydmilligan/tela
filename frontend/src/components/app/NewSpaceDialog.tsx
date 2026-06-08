import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { ApiError } from '../../lib/api'
import { useCreateSpace } from '../../lib/queries/spaces'
import { useOrgs } from '../../lib/queries/orgs'
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

interface NewSpaceDialogProps {
  open: boolean
  onOpenChange: (next: boolean) => void
}

const PERSONAL = 'personal'

export function NewSpaceDialog({ open, onOpenChange }: NewSpaceDialogProps) {
  const [name, setName] = useState('')
  const [owner, setOwner] = useState(PERSONAL)
  const [error, setError] = useState<string | null>(null)
  const [overQuota, setOverQuota] = useState(false)
  const navigate = useNavigate()
  const createSpace = useCreateSpace()
  const orgs = useOrgs()
  // Orgs the caller can actually create spaces in (a membership row).
  const myOrgs = (orgs.data ?? []).filter((o) => o.my_role != null)

  function handleClose(next: boolean) {
    if (!next) {
      setName('')
      setOwner(PERSONAL)
      setError(null)
      setOverQuota(false)
    }
    onOpenChange(next)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) {
      setError('Name is required.')
      return
    }
    setError(null)
    setOverQuota(false)
    try {
      const created = await createSpace.mutateAsync({
        name: trimmed,
        org_id: owner === PERSONAL ? undefined : Number(owner),
      })
      handleClose(false)
      void navigate({ to: '/spaces/$spaceId', params: { spaceId: created.id } })
    } catch (err) {
      if (err instanceof ApiError && err.status === 402) {
        setOverQuota(true)
        setError(err.message)
      } else {
        setError(err instanceof ApiError ? err.message : 'Failed to create space.')
      }
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create a new space</DialogTitle>
          <DialogDescription>
            Spaces hold a tree of pages. The slug is derived from the name.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="flex flex-col gap-[var(--space-3)]">
          <div className="flex flex-col gap-[var(--space-2)]">
            <label
              htmlFor="new-space-name"
              className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
            >
              Name
            </label>
            <Input
              id="new-space-name"
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Engineering"
              aria-invalid={error != null}
            />
          </div>

          {myOrgs.length > 0 ? (
            <div className="flex flex-col gap-[var(--space-2)]">
              <label
                htmlFor="new-space-owner"
                className="text-[length:var(--text-sm)] text-[var(--text-muted)]"
              >
                Owner
              </label>
              <Select
                id="new-space-owner"
                value={owner}
                onChange={(e) => setOwner(e.target.value)}
              >
                <option value={PERSONAL}>Personal</option>
                {myOrgs.map((o) => (
                  <option key={o.id} value={String(o.id)}>
                    {o.name}
                  </option>
                ))}
              </Select>
              <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)]">
                {owner === PERSONAL
                  ? 'Owned by you and counts against your personal plan.'
                  : 'Owned by the organization; its members can edit and it uses the org plan.'}
              </p>
            </div>
          ) : null}

          {error ? (
            <p
              role="alert"
              className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]"
            >
              {error}
              {overQuota ? (
                <>
                  {' '}
                  <button
                    type="button"
                    className="underline bg-transparent border-0 p-0 cursor-pointer text-[var(--accent)]"
                    onClick={() => {
                      handleClose(false)
                      void navigate({ to: '/settings' })
                    }}
                  >
                    View plan &amp; usage
                  </button>
                </>
              ) : null}
            </p>
          ) : null}

          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="ghost">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={createSpace.isPending}>
              {createSpace.isPending ? 'Creating…' : 'Create'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
