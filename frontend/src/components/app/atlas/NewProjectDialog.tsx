import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '../../ui/dialog'
import { Button } from '../../ui/button'
import { Input } from '../../ui/input'
import { Select } from '../../ui/select'
import { useSpaces } from '../../../lib/queries/spaces'
import { type AtlasCadence, type AtlasOwner, useCreateProject } from '../../../lib/queries/atlas'
import { SpacePicker, type SpaceChoice } from './SpacePicker'

const CADENCES: { value: AtlasCadence; label: string }[] = [
  { value: '', label: 'Manual — I run it' },
  { value: 'hourly', label: 'Automatic · hourly' },
  { value: 'daily', label: 'Automatic · daily' },
  { value: 'weekly', label: 'Automatic · weekly' },
  { value: 'monthly', label: 'Automatic · monthly' },
]

export function NewProjectDialog({
  open,
  onOpenChange,
  owners,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  owners: AtlasOwner[]
}) {
  const navigate = useNavigate()
  const create = useCreateProject()
  const spacesQ = useSpaces()
  const [name, setName] = useState('')
  const [ownerIdx, setOwnerIdx] = useState(0)
  const [output, setOutput] = useState<SpaceChoice>({})
  const [cadence, setCadence] = useState<AtlasCadence>('')
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    if (open) {
      setName('')
      setOwnerIdx(0)
      setOutput({})
      setCadence('')
      setErr(null)
    }
  }, [open])

  const owner = owners[ownerIdx]
  const canSubmit = useMemo(
    () => name.trim().length > 0 && owner != null && !create.isPending,
    [name, owner, create.isPending],
  )

  async function submit() {
    if (!owner) return
    setErr(null)
    // Output defaults to a new space named after the project when left untouched.
    const out =
      output.space_id != null
        ? { space_id: output.space_id }
        : { new_space_name: (output.new_space_name || name).trim() }
    try {
      const { project } = await create.mutateAsync({
        name: name.trim(),
        owner_kind: owner.kind,
        owner_id: owner.id,
        output: out,
        cadence,
        auto_update: cadence !== '',
      })
      onOpenChange(false)
      navigate({ to: '/atlas/projects/$projectId', params: { projectId: project.id } })
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Could not create the project.')
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New Atlas project</DialogTitle>
          <DialogDescription>
            A project bundles sources into one output space. You'll add the repos / Jira projects next, then run it.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-[var(--space-4)] py-[var(--space-2)]">
          <Field label="Project name">
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="compass" autoFocus />
          </Field>

          {owners.length > 1 && (
            <Field label="Owner" hint="Who manages it — you, or an org's admins.">
              <Select value={String(ownerIdx)} onChange={(e) => setOwnerIdx(Number(e.target.value))}>
                {owners.map((o, i) => (
                  <option key={`${o.kind}:${o.id}`} value={i}>
                    {o.kind === 'user' ? `${o.name} (personal)` : `${o.name} (org)`}
                  </option>
                ))}
              </Select>
            </Field>
          )}

          <FieldBlock label="Output space" hint={`Pick an existing space or name a new one. A new space is created ${owner?.kind === 'org' ? `in ${owner.name}` : 'as personal'} (the project's owner); defaults to the project name.`}>
            <SpacePicker
              spaces={(spacesQ.data ?? []).map((s) => ({ id: s.id, name: s.name }))}
              value={output}
              onChange={setOutput}
              placeholder={name.trim() ? `Default: “${name.trim()}” (new space)` : 'Search a space, or name a new one…'}
            />
          </FieldBlock>

          <Field label="Refresh">
            <Select value={cadence} onChange={(e) => setCadence(e.target.value as AtlasCadence)}>
              {CADENCES.map((c) => (
                <option key={c.value} value={c.value}>{c.label}</option>
              ))}
            </Select>
          </Field>

          {err && <p className="text-[length:var(--text-sm)] text-[var(--accent-negative-fg)]">{err}</p>}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button variant="primary" disabled={!canSubmit} onClick={submit}>
            {create.isPending && <Loader2 className="size-[var(--space-4)] motion-safe:animate-spin" />}
            Create project
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// A labelled field whose control is a real form element — uses <label> so the
// caption focuses it.
export function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <label className="flex flex-col gap-[var(--space-1)]">
      <span className="text-[length:var(--text-sm)] font-medium text-[var(--text-primary)]">{label}</span>
      {children}
      {hint && <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">{hint}</span>}
    </label>
  )
}

// Like Field but a plain <div> — for composite controls (e.g. the SpacePicker
// combobox) where a wrapping <label> would hijack clicks on the dropdown.
export function FieldBlock({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-[var(--space-1)]">
      <span className="text-[length:var(--text-sm)] font-medium text-[var(--text-primary)]">{label}</span>
      {children}
      {hint && <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">{hint}</span>}
    </div>
  )
}
