import { Checkbox } from '../ui/checkbox'
import { useUiPrefs, setUiPref } from '../../lib/ui-prefs'

// Per-device UI behavior toggles (#27, #28). These are browser preferences, not
// account settings, so they persist in localStorage via ui-prefs — no server
// round-trip, and useUiPrefs keeps this screen in sync with the live setting.
export function SettingsBehaviorTab() {
  const prefs = useUiPrefs()
  return (
    <section
      aria-labelledby="settings-behavior"
      className="flex flex-col gap-[var(--space-4)]"
    >
      <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
        Small interaction preferences for the sidebar and editor. These are saved
        on this device.
      </p>

      <label className="flex items-start gap-[var(--space-3)] rounded-[var(--radius-md)] border border-[var(--border-subtle)] px-[var(--space-4)] py-[var(--space-3)]">
        <Checkbox
          checked={prefs.newChildEditMode}
          aria-label="Open new child pages in edit mode"
          onCheckedChange={(v) => setUiPref('newChildEditMode', v === true)}
          className="mt-[2px]"
        />
        <span className="flex flex-col gap-[1px]">
          <span className="text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium">
            New pages open in edit mode
          </span>
          <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">
            When you create a page with “New child page”, land straight in the
            editor instead of the read view.
          </span>
        </span>
      </label>

      <label className="flex items-start gap-[var(--space-3)] rounded-[var(--radius-md)] border border-[var(--border-subtle)] px-[var(--space-4)] py-[var(--space-3)]">
        <Checkbox
          checked={prefs.clickExpandsChildren}
          aria-label="Clicking a page with children also expands it"
          onCheckedChange={(v) => setUiPref('clickExpandsChildren', v === true)}
          className="mt-[2px]"
        />
        <span className="flex flex-col gap-[1px]">
          <span className="text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium">
            Clicking a page also expands its children
          </span>
          <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">
            In the sidebar, clicking a page that has sub-pages opens it and expands
            its subtree. The chevron still toggles on its own.
          </span>
        </span>
      </label>
    </section>
  )
}
