import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { ApiError } from '../../lib/api'
import { useCreateSpace } from '../../lib/queries/spaces'
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

interface NewSpaceDialogProps {
  open: boolean
  onOpenChange: (next: boolean) => void
}

export function NewSpaceDialog({ open, onOpenChange }: NewSpaceDialogProps) {
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()
  const createSpace = useCreateSpace()

  function handleClose(next: boolean) {
    if (!next) {
      setName('')
      setError(null)
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
    try {
      const created = await createSpace.mutateAsync({ name: trimmed })
      handleClose(false)
      void navigate({ to: '/spaces/$spaceId', params: { spaceId: created.id } })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to create space.')
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
            {error ? (
              <p className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]">
                {error}
              </p>
            ) : null}
          </div>
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
