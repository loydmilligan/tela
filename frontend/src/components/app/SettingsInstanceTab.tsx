import { useState } from 'react'
import {
  useInstanceSettings,
  useUpdateInstanceSettings,
} from '../../lib/queries/instance-settings'
import { Checkbox } from '../ui/checkbox'
import { Input } from '../ui/input'
import { Select } from '../ui/select'
import { Button } from '../ui/button'

// Instance-admin runtime configuration, backed by the instance_settings store.
// Changes apply immediately with no redeploy.
export function SettingsInstanceTab() {
  const settings = useInstanceSettings()
  const update = useUpdateInstanceSettings()

  // Absent or "true" = open (the default for an open team wiki); only the
  // literal "false" closes registration.
  const registrationOpen = settings.data?.['registration_open'] !== 'false'
  const aiDisabled = settings.data?.['ai.disabled'] === '1'

  return (
    <section
      aria-labelledby="settings-instance"
      className="flex flex-col gap-[var(--space-5)]"
    >
      <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
        Instance-wide settings. Changes apply immediately — no redeploy.
      </p>

      {settings.isLoading ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">Loading…</p>
      ) : settings.isError ? (
        <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
          Couldn’t load instance settings.
        </p>
      ) : (
        <>
          <div className="flex flex-col rounded-[var(--radius-md)] border border-[var(--border-subtle)] overflow-hidden">
            <label className="flex items-start gap-[var(--space-3)] px-[var(--space-4)] py-[var(--space-3)] cursor-pointer">
              <Checkbox
                checked={registrationOpen}
                disabled={update.isPending}
                aria-label="Allow self-registration"
                onCheckedChange={(v) =>
                  update.mutate({ registration_open: v === true ? 'true' : 'false' })
                }
              />
              <span className="flex flex-col gap-[var(--space-1)]">
                <span className="text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium">
                  Allow self-registration
                </span>
                <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
                  When off, new users can’t sign up themselves; instance admins can still
                  create accounts directly.
                </span>
              </span>
            </label>

            <label className="flex items-start gap-[var(--space-3)] px-[var(--space-4)] py-[var(--space-3)] border-t border-[var(--border-subtle)] cursor-pointer">
              <Checkbox
                checked={aiDisabled}
                disabled={update.isPending}
                aria-label="Pause AI features"
                onCheckedChange={(v) =>
                  update.mutate({ 'ai.disabled': v === true ? '1' : '0' })
                }
              />
              <span className="flex flex-col gap-[var(--space-1)]">
                <span className="text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium">
                  Pause AI features
                </span>
                <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
                  Stops “ask your docs” and semantic search from calling the AI backend —
                  use while that service is under maintenance so it fails calmly, not loudly.
                  Pair it with a banner below.
                </span>
              </span>
            </label>
          </div>

          <MaintenanceNoticeEditor
            initialNotice={settings.data?.['maintenance.notice'] ?? ''}
            initialLevel={settings.data?.['maintenance.level'] === 'warning' ? 'warning' : 'info'}
            ready={settings.isSuccess}
            saving={update.isPending}
            onSave={(notice, level) =>
              update.mutate({ 'maintenance.notice': notice, 'maintenance.level': level })
            }
          />
        </>
      )}
    </section>
  )
}

// The app-wide banner an admin sets — any message ("AI is paused for
// maintenance"). Saving an empty notice clears it. Local-edit + Save so it isn't
// written on every keystroke.
function MaintenanceNoticeEditor({
  initialNotice,
  initialLevel,
  ready,
  saving,
  onSave,
}: {
  initialNotice: string
  initialLevel: 'info' | 'warning'
  ready: boolean
  saving: boolean
  onSave: (notice: string, level: 'info' | 'warning') => void
}) {
  const [notice, setNotice] = useState(initialNotice)
  const [level, setLevel] = useState<'info' | 'warning'>(initialLevel)
  // Hydrate once from the loaded settings (adjust-state-when-a-prop-changes).
  const [hydrated, setHydrated] = useState(false)
  if (ready && !hydrated) {
    setNotice(initialNotice)
    setLevel(initialLevel)
    setHydrated(true)
  }

  const dirty = notice !== initialNotice || level !== initialLevel

  return (
    <div className="flex flex-col gap-[var(--space-3)] rounded-[var(--radius-md)] border border-[var(--border-subtle)] p-[var(--space-4)]">
      <div className="flex flex-col gap-[var(--space-1)]">
        <span className="text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium">
          Maintenance banner
        </span>
        <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
          A soft notice shown to everyone, top of the app + login. Leave empty to hide it.
        </span>
      </div>
      <div className="flex items-start gap-[var(--space-2)]">
        <div className="flex-1 min-w-0">
          <Input
            value={notice}
            onChange={(e) => setNotice(e.target.value)}
            placeholder="e.g. AI is temporarily unavailable for maintenance."
            aria-label="Maintenance notice"
          />
        </div>
        <Select
          value={level}
          onChange={(e) => setLevel(e.target.value as 'info' | 'warning')}
          aria-label="Notice level"
          className="w-[8rem] shrink-0"
        >
          <option value="info">Info</option>
          <option value="warning">Warning</option>
        </Select>
        <Button
          type="button"
          variant="secondary"
          disabled={saving || !dirty}
          onClick={() => onSave(notice.trim(), level)}
        >
          {saving ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </div>
  )
}
