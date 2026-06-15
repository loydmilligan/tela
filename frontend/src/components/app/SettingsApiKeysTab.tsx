import { useState } from 'react'
import { Copy, KeyRound, ShieldAlert } from 'lucide-react'
import { ApiError } from '../../lib/api'
import {
  useApiKeys,
  useCreateApiKey,
  useRevokeApiKey,
  type CreateApiKeyInput,
} from '../../lib/queries/api-keys'
import { useSpaces } from '../../lib/queries/spaces'
import { useMe } from '../../lib/queries/auth'
import { localDateFromSqlite, relativeTimeFromSqlite } from '../../lib/relativeTime'
import type { ApiKeyRow, ApiKeyScope } from '../../lib/types'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../ui/dialog'
import { Input } from '../ui/input'
import { Select } from '../ui/select'
import { ToggleGroup, ToggleGroupItem } from '../ui/toggle'
import { cn } from '../../lib/utils'
import { normaliseExpiryInput } from './ShareManagerSheet-utils'
import { DOCS } from '../../lib/docs'

const NAME_MAX_LEN = 64
const ALL_SPACES_VALUE = '__all__'
const COPIED_FLASH_MS = 1200

const SCOPE_DESCRIPTIONS: Record<ApiKeyScope, string> = {
  read: 'Read-only — GETs on pages, spaces, search, comments.',
  write: 'Read + write — create/edit pages and comments, run imports.',
  admin: 'Full instance admin — manage users, spaces, and other API keys.',
}

export function SettingsApiKeysTab() {
  const keys = useApiKeys()
  const [created, setCreated] = useState<ApiKeyRow | null>(null)

  return (
    <section
      aria-labelledby="settings-api-keys"
      className="flex flex-col gap-[var(--space-5)]"
    >
      <header className="flex flex-col gap-[var(--space-1)]" id="settings-api-keys">
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
          Personal access tokens for headless agents and scripts. Bearer-auth
          via{' '}
          <code className="font-[family-name:var(--font-mono)]">
            Authorization: Bearer tela_pat_…
          </code>
          . Tokens are shown once on creation — store them somewhere safe.
        </p>
        <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)]">
          <a
            href={DOCS.mcp}
            target="_blank"
            rel="noreferrer"
            className="text-[var(--accent)] underline underline-offset-2"
          >
            Connect an agent — MCP setup guide →
          </a>
        </p>
      </header>

      <CreateApiKeyForm onCreated={setCreated} />

      <ApiKeysList
        keys={keys.data ?? []}
        loading={keys.isLoading}
        isError={keys.isError}
      />

      <ShowOnceDialog
        keyRow={created}
        onOpenChange={(open) => {
          if (!open) setCreated(null)
        }}
      />
    </section>
  )
}

interface CreateApiKeyFormProps {
  onCreated: (key: ApiKeyRow) => void
}

function CreateApiKeyForm({ onCreated }: CreateApiKeyFormProps) {
  const spaces = useSpaces()
  const me = useMe()
  const create = useCreateApiKey()
  const [name, setName] = useState('')
  const [scope, setScope] = useState<ApiKeyScope>('read')
  const [spaceValue, setSpaceValue] = useState<string>(ALL_SPACES_VALUE)
  const [expiresRaw, setExpiresRaw] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [fieldError, setFieldError] = useState<'name' | 'expires' | null>(null)

  function resetForm() {
    setName('')
    setScope('read')
    setSpaceValue(ALL_SPACES_VALUE)
    setExpiresRaw('')
    setError(null)
    setFieldError(null)
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    const trimmedName = name.trim()
    if (!trimmedName) {
      setError('Name is required.')
      setFieldError('name')
      return
    }
    if (trimmedName.length > NAME_MAX_LEN) {
      setError(`Name must be at most ${NAME_MAX_LEN} characters.`)
      setFieldError('name')
      return
    }
    const expiresWire = normaliseExpiryInput(expiresRaw)
    if (expiresRaw.trim().length > 0 && !expiresWire) {
      setError('Expiry must be a YYYY-MM-DD HH:MM:SS timestamp.')
      setFieldError('expires')
      return
    }
    setError(null)
    setFieldError(null)
    const input: CreateApiKeyInput = {
      name: trimmedName,
      scope,
      space_id:
        spaceValue === ALL_SPACES_VALUE ? null : Number(spaceValue),
      expires_at: expiresWire || null,
    }
    try {
      const created = await create.mutateAsync(input)
      onCreated(created)
      resetForm()
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.code === 'bad_request' && /expires_at/i.test(err.message)) {
          setFieldError('expires')
          setError('Expiry must be in the future.')
          return
        }
        if (err.code === 'space_not_found') {
          setError('Selected space no longer exists. Reload the page.')
          return
        }
        setError(err.message)
        return
      }
      setError('Failed to create the API key. Try again.')
    }
  }

  return (
    <section
      aria-label="Create API key"
      className={cn(
        'rounded-[var(--radius-md)] border border-[var(--border-subtle)]',
        'bg-[var(--surface-1)]',
        'px-[var(--space-4)] py-[var(--space-4)]',
      )}
    >
      <h2 className="m-0 mb-[var(--space-3)] font-[family-name:var(--font-sans)] text-[length:var(--text-base)] leading-[var(--leading-tight)] text-[var(--text-primary)]">
        Create a new key
      </h2>
      <form onSubmit={handleSubmit} className="flex flex-col gap-[var(--space-4)]" noValidate>
        <Field label="Name" htmlFor="apikey-name">
          <Input
            id="apikey-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. claude-code laptop"
            maxLength={NAME_MAX_LEN}
            autoComplete="off"
            aria-invalid={fieldError === 'name' ? true : undefined}
          />
        </Field>

        <Field label="Scope">
          <ToggleGroup
            type="single"
            value={scope}
            onValueChange={(next) => {
              if (next === 'read' || next === 'write' || next === 'admin') {
                setScope(next)
              }
            }}
            aria-label="API key scope"
          >
            <ToggleGroupItem value="read">Read</ToggleGroupItem>
            <ToggleGroupItem value="write">Write</ToggleGroupItem>
            {/* Admin scope = full instance-admin powers; only offered to instance
                admins (the backend rejects it for anyone else). */}
            {me.data?.is_instance_admin ? (
              <ToggleGroupItem value="admin">Admin</ToggleGroupItem>
            ) : null}
          </ToggleGroup>
          <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
            {SCOPE_DESCRIPTIONS[scope]}
          </p>
        </Field>

        <Field label="Space" htmlFor="apikey-space">
          {spaces.isLoading ? (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
              Loading spaces…
            </p>
          ) : spaces.isError || !spaces.data ? (
            <p
              role="alert"
              className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
            >
              Couldn't load spaces.
            </p>
          ) : (
            <Select
              id="apikey-space"
              value={spaceValue}
              onChange={(e) => setSpaceValue(e.target.value)}
            >
              <option value={ALL_SPACES_VALUE}>All spaces</option>
              {spaces.data.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </Select>
          )}
        </Field>

        <Field label="Expires (optional)" htmlFor="apikey-expires">
          <Input
            id="apikey-expires"
            type="text"
            value={expiresRaw}
            onChange={(e) => setExpiresRaw(e.target.value)}
            placeholder="YYYY-MM-DD HH:MM:SS (UTC)"
            autoComplete="off"
            aria-invalid={fieldError === 'expires' ? true : undefined}
          />
          <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
            Leave blank for a key that never expires.
          </p>
        </Field>

        {error ? (
          <p
            role="alert"
            className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
          >
            {error}
          </p>
        ) : null}

        <div className="flex">
          <Button type="submit" variant="primary" disabled={create.isPending}>
            <KeyRound width={14} height={14} />
            <span>{create.isPending ? 'Creating…' : 'Create key'}</span>
          </Button>
        </div>
      </form>
    </section>
  )
}

interface ApiKeysListProps {
  keys: ApiKeyRow[]
  loading: boolean
  isError: boolean
}

function ApiKeysList({ keys, loading, isError }: ApiKeysListProps) {
  if (loading) {
    return (
      <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
        Loading API keys…
      </p>
    )
  }
  if (isError) {
    return (
      <p
        role="alert"
        className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]"
      >
        Couldn't load API keys.
      </p>
    )
  }
  const active = keys.filter((k) => !k.revoked_at)
  if (active.length === 0) {
    return (
      <p
        className={cn(
          'm-0 rounded-[var(--radius-md)] border border-dashed border-[var(--border-subtle)]',
          'px-[var(--space-4)] py-[var(--space-5)]',
          'text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]',
          'text-center',
        )}
      >
        No API keys yet. Create one above.
      </p>
    )
  }
  return (
    <ul className="m-0 p-0 list-none flex flex-col gap-[var(--space-2)]">
      {active.map((k) => (
        <ApiKeyRowItem key={k.id} row={k} />
      ))}
    </ul>
  )
}

type RowMode = 'idle' | 'confirm-revoke'

function ApiKeyRowItem({ row }: { row: ApiKeyRow }) {
  const spaces = useSpaces()
  const revoke = useRevokeApiKey()
  const [mode, setMode] = useState<RowMode>('idle')
  const [error, setError] = useState<string | null>(null)

  const spaceLabel =
    row.space_id == null
      ? 'All spaces'
      : (spaces.data?.find((s) => s.id === row.space_id)?.name ??
        `Space #${row.space_id}`)

  const lastUsedLabel =
    row.last_used_at != null ? relativeTimeFromSqlite(row.last_used_at) : 'never'
  const expiresLabel =
    row.expires_at != null ? localDateFromSqlite(row.expires_at) : 'never'

  async function handleRevoke() {
    setError(null)
    try {
      await revoke.mutateAsync(row.id)
      // The row vanishes from the list on refetch — no further state cleanup.
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('Failed to revoke. Try again.')
      }
      setMode('idle')
    }
  }

  return (
    <li
      className={cn(
        'm-0 list-none',
        'flex flex-col gap-[var(--space-2)]',
        'px-[var(--space-3)] py-[var(--space-3)]',
        'rounded-[var(--radius-sm)]',
        'border border-[var(--border-subtle)] bg-[var(--surface-1)]',
      )}
    >
      <div className="flex items-start gap-[var(--space-3)]">
        <div className="flex-1 min-w-0 flex flex-col gap-[2px]">
          <div className="flex items-center gap-[var(--space-2)] min-w-0 flex-wrap">
            <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium font-[family-name:var(--font-sans)]">
              {row.name}
            </span>
            <ScopeBadge scope={row.scope} />
            <Badge variant="muted">{spaceLabel}</Badge>
          </div>
          <span
            className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-mono)]"
            title="Key prefix (first 8 chars of the random body)"
          >
            tela_pat_{row.key_prefix}…
          </span>
          <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
            Last used {lastUsedLabel} · Expires {expiresLabel} · Created{' '}
            {localDateFromSqlite(row.created_at)}
          </span>
        </div>
        {mode === 'idle' ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => {
              setError(null)
              setMode('confirm-revoke')
            }}
            aria-label={`Revoke ${row.name}`}
            disabled={revoke.isPending}
          >
            Revoke
          </Button>
        ) : null}
      </div>

      {mode === 'confirm-revoke' ? (
        <div
          className={cn(
            'flex items-center justify-between gap-[var(--space-2)]',
            'rounded-[var(--radius-sm)] bg-[var(--surface-2)]',
            'px-[var(--space-2)] py-[var(--space-2)]',
          )}
        >
          <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
            Revoke <strong>{row.name}</strong>? This invalidates the key
            immediately and cannot be undone.
          </span>
          <div className="flex items-center gap-[var(--space-1)] shrink-0">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setMode('idle')}
              disabled={revoke.isPending}
            >
              Cancel
            </Button>
            <Button
              type="button"
              variant="danger"
              size="sm"
              onClick={() => void handleRevoke()}
              disabled={revoke.isPending}
            >
              {revoke.isPending ? 'Revoking…' : 'Revoke'}
            </Button>
          </div>
        </div>
      ) : null}

      {error ? (
        <p
          role="alert"
          className="m-0 text-[length:var(--text-xs)] text-[var(--danger)]"
        >
          {error}
        </p>
      ) : null}
    </li>
  )
}

function ScopeBadge({ scope }: { scope: ApiKeyScope }) {
  // `admin` is the only scope that deserves the accent tint — it's the loaded
  // gun. `read` and `write` stay muted so the badge row reads as a label,
  // not a warning.
  return (
    <Badge variant={scope === 'admin' ? 'accent' : 'muted'}>
      {scope}
    </Badge>
  )
}

interface ShowOnceDialogProps {
  keyRow: ApiKeyRow | null
  onOpenChange: (open: boolean) => void
}

function ShowOnceDialog({ keyRow, onOpenChange }: ShowOnceDialogProps) {
  const [copied, setCopied] = useState(false)

  async function handleCopy() {
    if (!keyRow?.key) return
    try {
      if (!navigator.clipboard?.writeText) return
      await navigator.clipboard.writeText(keyRow.key)
      setCopied(true)
      window.setTimeout(() => setCopied(false), COPIED_FLASH_MS)
    } catch {
      // Best-effort copy; the user can also select the text manually.
    }
  }

  return (
    <Dialog
      open={keyRow != null}
      onOpenChange={(open) => {
        if (!open) setCopied(false)
        onOpenChange(open)
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>API key created</DialogTitle>
          <DialogDescription>
            This is the only time the full key will be shown. Store it
            somewhere safe — once you close this dialog, it cannot be recovered.
          </DialogDescription>
        </DialogHeader>
        {keyRow ? (
          <div className="flex flex-col gap-[var(--space-3)]">
            <div
              className={cn(
                'flex items-start gap-[var(--space-2)]',
                'rounded-[var(--radius-sm)]',
                'border border-[var(--accent)] bg-[var(--surface-2)]',
                'px-[var(--space-3)] py-[var(--space-2)]',
              )}
            >
              <ShieldAlert
                aria-hidden
                width={16}
                height={16}
                className="mt-[2px] shrink-0 text-[var(--accent)]"
              />
              <span className="text-[length:var(--text-xs)] text-[var(--text-primary)] font-[family-name:var(--font-sans)] leading-[var(--leading-relaxed)]">
                Treat this like a password. Anyone with the key can act as
                your account at the chosen scope.
              </span>
            </div>
            <div className="flex flex-col gap-[var(--space-1)]">
              <label
                htmlFor="apikey-show-once"
                className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
              >
                Full key
              </label>
              <Input
                id="apikey-show-once"
                readOnly
                value={keyRow.key ?? ''}
                className="font-[family-name:var(--font-mono)] text-[length:var(--text-xs)]"
                onFocus={(e) => e.currentTarget.select()}
              />
            </div>
            <DialogFooter>
              <Button
                type="button"
                variant="secondary"
                onClick={() => void handleCopy()}
              >
                <Copy width={14} height={14} />
                <span>{copied ? 'Copied!' : 'Copy'}</span>
              </Button>
              <Button
                type="button"
                variant="primary"
                onClick={() => onOpenChange(false)}
              >
                I've saved it
              </Button>
            </DialogFooter>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

interface FieldProps {
  label: string
  htmlFor?: string
  children: React.ReactNode
}

function Field({ label, htmlFor, children }: FieldProps) {
  return (
    <div className="flex flex-col gap-[var(--space-2)]">
      <label
        htmlFor={htmlFor}
        className="text-[length:var(--text-sm)] font-medium text-[var(--text-primary)] font-[family-name:var(--font-sans)]"
      >
        {label}
      </label>
      {children}
    </div>
  )
}
