import {
  useInstanceSettings,
  useUpdateInstanceSettings,
} from '../../lib/queries/instance-settings'
import { Checkbox } from '../ui/checkbox'

// Instance-admin runtime configuration, backed by the instance_settings store.
// Changes apply immediately with no redeploy. Today this exposes the
// self-registration toggle; new instance settings drop in as more rows here.
export function SettingsInstanceTab() {
  const settings = useInstanceSettings()
  const update = useUpdateInstanceSettings()

  // Absent or "true" = open (the default for an open team wiki); only the
  // literal "false" closes registration.
  const registrationOpen = settings.data?.['registration_open'] !== 'false'

  return (
    <section
      aria-labelledby="settings-instance"
      className="flex flex-col gap-[var(--space-4)]"
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
        </div>
      )}
    </section>
  )
}
