import { useState } from 'react'
import { BadgeCheck, KeyRound, Lock } from 'lucide-react'
import { Card } from '../ui/card'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { TextArea } from '../ui/textarea'
import { ApiError } from '../../lib/api'
import { useClearLicense, useLicense, useSetLicense } from '../../lib/queries/license'

// Instance-admin "License" tab — install / view / remove the self-host Enterprise
// key (internal/ee). Community (no key) is the default; a valid key unlocks the
// ee-gated features. When the key is pinned via TELA_LICENSE_KEY the panel is
// read-only (env always wins on the next boot).
export function SettingsLicenseTab() {
  const info = useLicense()
  const setLicense = useSetLicense()
  const clearLicense = useClearLicense()
  const [token, setToken] = useState('')
  const [error, setError] = useState<string | null>(null)

  const lic = info.data?.license
  const envLocked = info.data?.env_locked ?? false
  const active = lic?.valid ?? false

  async function install() {
    setError(null)
    try {
      await setLicense.mutateAsync(token.trim())
      setToken('')
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not install the license key.')
    }
  }

  async function remove() {
    setError(null)
    try {
      await clearLicense.mutateAsync()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not remove the license key.')
    }
  }

  return (
    <div className="flex flex-col gap-[var(--space-6)] max-w-[var(--measure,60ch)]">
      <header className="flex flex-col gap-[var(--space-1)]">
        <h2 className="m-0 text-[length:var(--text-lg)] font-medium text-[var(--text-primary)]">
          License
        </h2>
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          tela is the open Community edition by default. An Enterprise license key unlocks
          SSO, audit, and the other team-of-record features on this instance.
        </p>
      </header>

      {info.isError ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
          Could not load license status.
        </p>
      ) : null}

      {/* Current edition */}
      <Card className="flex flex-col gap-[var(--space-3)] p-[var(--space-4)]">
        <div className="flex items-center justify-between gap-[var(--space-2)]">
          <div className="flex items-center gap-[var(--space-2)]">
            {active ? (
              <BadgeCheck width={18} height={18} className="text-[var(--accent)]" aria-hidden />
            ) : (
              <KeyRound width={18} height={18} className="text-[var(--text-muted)]" aria-hidden />
            )}
            <span className="font-medium text-[var(--text-primary)]">
              {info.isPending
                ? 'Checking license…'
                : active
                  ? `Enterprise — ${lic?.tier}`
                  : 'Community edition'}
            </span>
          </div>
          {info.isPending ? null : active ? (
            <Badge variant="accent">Active</Badge>
          ) : (
            <Badge>Free</Badge>
          )}
        </div>

        {active && lic ? (
          <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-[var(--space-4)] gap-y-[var(--space-1)] text-[length:var(--text-sm)]">
            {lic.customer ? (
              <>
                <dt className="text-[var(--text-muted)]">Licensed to</dt>
                <dd className="m-0 text-[var(--text-primary)]">{lic.customer}</dd>
              </>
            ) : null}
            {lic.seats > 0 ? (
              <>
                <dt className="text-[var(--text-muted)]">Seats</dt>
                <dd className="m-0 text-[var(--text-primary)]">{lic.seats}</dd>
              </>
            ) : null}
            <dt className="text-[var(--text-muted)]">Features</dt>
            <dd className="m-0 text-[var(--text-primary)]">
              {lic.features.includes('*') ? 'All Enterprise features' : lic.features.join(', ') || '—'}
            </dd>
            <dt className="text-[var(--text-muted)]">Expires</dt>
            <dd className="m-0 text-[var(--text-primary)]">{lic.expires_at ? lic.expires_at.slice(0, 10) : 'Never'}</dd>
          </dl>
        ) : (
          <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
            No license installed. The whole product works without one — Enterprise features
            stay locked until a key is added.
          </p>
        )}

        {active && !envLocked ? (
          <div>
            <Button
              variant="ghost"
              onClick={remove}
              disabled={clearLicense.isPending}
            >
              Remove license
            </Button>
          </div>
        ) : null}
      </Card>

      {/* Install / replace */}
      {envLocked ? (
        <p className="m-0 flex items-center gap-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
          <Lock width={15} height={15} aria-hidden />
          The license key is set via the <code>TELA_LICENSE_KEY</code> environment variable and
          can&apos;t be changed here.
        </p>
      ) : (
        <div className="flex flex-col gap-[var(--space-2)]">
          <label htmlFor="license-token" className="text-[length:var(--text-sm)] font-medium text-[var(--text-primary)]">
            {active ? 'Replace license key' : 'Install a license key'}
          </label>
          <TextArea
            id="license-token"
            value={token}
            onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) => setToken(e.target.value)}
            placeholder="tela_lic_…"
            rows={3}
            className="font-mono text-[length:var(--text-xs)]"
          />
          {error ? (
            <p className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">{error}</p>
          ) : null}
          <div>
            <Button onClick={install} disabled={setLicense.isPending || token.trim() === ''}>
              {active ? 'Replace' : 'Install'}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
