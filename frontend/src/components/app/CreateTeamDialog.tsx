import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useCreateOrg } from '../../lib/queries/orgs'
import { ApiError } from '../../lib/api'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '../ui/dialog'
import { Button } from '../ui/button'
import { Input } from '../ui/input'

// Self-serve "create a team": spin up an organization (free tier), become its
// admin, then land on its manage page to invite teammates + upgrade to Team.
export function CreateTeamDialog({ trigger }: { trigger: React.ReactNode }) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const createOrg = useCreateOrg()
  const navigate = useNavigate()

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    const n = name.trim()
    if (!n) return
    try {
      const org = await createOrg.mutateAsync({ name: n })
      setOpen(false)
      setName('')
      void navigate({ to: '/settings/orgs/$orgId', params: { orgId: org.id } })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not create the team.')
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>{trigger}</DialogTrigger>
      <DialogContent>
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>Create a team</DialogTitle>
            <DialogDescription>
              Spin up an organization, invite teammates, then upgrade to the Team plan. It starts
              free — no card required.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-[var(--space-2)] py-[var(--space-4)]">
            <label
              htmlFor="team-name"
              className="text-[length:var(--text-sm)] font-medium text-[var(--text-primary)]"
            >
              Team name
            </label>
            <Input
              id="team-name"
              value={name}
              onChange={(e: React.ChangeEvent<HTMLInputElement>) => setName(e.target.value)}
              placeholder="Acme Inc"
              autoFocus
              maxLength={200}
            />
            {error ? (
              <p className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">{error}</p>
            ) : null}
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="ghost">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={createOrg.isPending || name.trim() === ''}>
              {createOrg.isPending ? 'Creating…' : 'Create team'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
