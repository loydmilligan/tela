import { useRef, useState } from 'react'
import { type ShareDTO } from '../../lib/queries/share'
import { Button } from '../ui/button'
import { Checkbox } from '../ui/checkbox'
import { Input } from '../ui/input'
import { cn } from '../../lib/utils'
import {
  normaliseExpiryInput,
  shareErrorMessage,
} from './ShareManagerSheet-utils'

interface CreateShareFormProps {
  pending: boolean
  error: Error | null
  onCreate: (input: {
    include_descendants: boolean
    password?: string
    expires_at?: string
  }) => Promise<ShareDTO>
  onReset: () => void
}

export function CreateShareForm({
  pending,
  error,
  onCreate,
  onReset,
}: CreateShareFormProps) {
  const [includeDescendants, setIncludeDescendants] = useState(false)
  const [password, setPassword] = useState('')
  const [expiresAt, setExpiresAt] = useState('')
  const checkboxRef = useRef<HTMLButtonElement | null>(null)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const wire = normaliseExpiryInput(expiresAt)
    try {
      await onCreate({
        include_descendants: includeDescendants,
        password: password.length > 0 ? password : undefined,
        expires_at: wire.length > 0 ? wire : undefined,
      })
      // Clear form on success + return focus to the first field so the user
      // can immediately create another share with the same defaults.
      setIncludeDescendants(false)
      setPassword('')
      setExpiresAt('')
      checkboxRef.current?.focus()
    } catch {
      // Error surface is the `error` prop — TanStack's mutate cycle already
      // recorded it on the hook.
    }
  }

  return (
    <form
      onSubmit={(e) => void handleSubmit(e)}
      onReset={() => {
        setIncludeDescendants(false)
        setPassword('')
        setExpiresAt('')
        onReset()
      }}
      className={cn(
        'flex flex-col gap-[var(--space-3)]',
        'rounded-[var(--radius-md)] border border-[var(--border-subtle)]',
        'bg-[var(--surface-1)]',
        'px-[var(--space-3)] py-[var(--space-3)]',
      )}
      aria-labelledby="create-share-heading"
    >
      <h3
        id="create-share-heading"
        className="m-0 text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
      >
        Create a new share
      </h3>

      <label className="flex items-center gap-[var(--space-2)]">
        <Checkbox
          ref={checkboxRef}
          checked={includeDescendants}
          onCheckedChange={(v) => setIncludeDescendants(v === true)}
          disabled={pending}
          aria-label="Include child pages"
        />
        <span className="text-[length:var(--text-sm)] text-[var(--text-primary)] font-[family-name:var(--font-sans)]">
          Include child pages
        </span>
      </label>

      <label className="flex flex-col gap-[var(--space-1)]">
        <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
          Password (optional)
        </span>
        <Input
          size="sm"
          type="text"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="Optional password"
          aria-label="Optional password"
          disabled={pending}
        />
      </label>

      <label className="flex flex-col gap-[var(--space-1)]">
        <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
          Expires at (UTC, optional)
        </span>
        <Input
          size="sm"
          type="text"
          value={expiresAt}
          onChange={(e) => setExpiresAt(e.target.value)}
          placeholder="YYYY-MM-DD HH:MM:SS — leave empty for none"
          aria-label="Optional expiry"
          disabled={pending}
        />
      </label>

      {error ? (
        <p
          role="alert"
          className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]"
        >
          {shareErrorMessage(error)}
        </p>
      ) : null}

      <div className="flex items-center justify-end">
        <Button type="submit" variant="primary" size="sm" disabled={pending}>
          {pending ? 'Creating…' : 'Create share'}
        </Button>
      </div>
    </form>
  )
}
