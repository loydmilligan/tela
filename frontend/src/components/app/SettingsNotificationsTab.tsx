import {
  useNotificationPrefs,
  useUpdateNotificationPref,
  type NotificationPref,
} from '../../lib/queries/notification-prefs'
import { useAutowatch, useSetAutowatch } from '../../lib/queries/subscriptions'
import { useMe } from '../../lib/queries/auth'
import { Checkbox } from '../ui/checkbox'
import { cn } from '../../lib/utils'

// The event types + channels the matrix renders. Mirrors the backend's
// notificationEventTypes / notificationChannels; adding one there + here exposes
// it. Both in-app and email are delivered. `adminOnly` rows show only to instance
// admins (the recipient set the backend emits them to).
const EVENTS: { type: string; label: string; desc: string; adminOnly?: boolean }[] = [
  { type: 'mention', label: 'Mentions', desc: 'When someone @-mentions you on a page.' },
  {
    type: 'page_updated',
    label: 'Page updates',
    desc: 'When a page or space you follow changes.',
  },
  {
    type: 'page_created',
    label: 'New pages',
    desc: 'When a new page is added to a space you follow.',
  },
  {
    type: 'comment_reply',
    label: 'Comment replies',
    desc: 'When someone replies to your comment.',
  },
  {
    type: 'space_added',
    label: 'Added to a space',
    desc: 'When someone gives you access to a space.',
  },
  {
    type: 'atlas_run',
    label: 'Atlas runs',
    desc: 'When an Atlas generation run you manage finishes or fails.',
  },
  {
    type: 'user_registered',
    label: 'New signups',
    desc: 'When a new account confirms its email on this instance.',
    adminOnly: true,
  },
]
const CHANNELS: { channel: string; label: string }[] = [
  { channel: 'inapp', label: 'In-app' },
  { channel: 'email', label: 'Email' },
]

export function SettingsNotificationsTab() {
  const prefs = useNotificationPrefs()
  const update = useUpdateNotificationPref()
  const autowatch = useAutowatch()
  const setAutowatch = useSetAutowatch()
  const me = useMe()
  const events = EVENTS.filter((ev) => !ev.adminOnly || me.data?.is_instance_admin)

  const enabled = (eventType: string, channel: string): boolean =>
    prefs.data?.find((p) => p.event_type === eventType && p.channel === channel)?.enabled ?? true

  function toggle(pref: NotificationPref) {
    update.mutate(pref)
  }

  return (
    <section aria-labelledby="settings-notifications" className="flex flex-col gap-[var(--space-4)]">
      <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)] leading-[var(--leading-relaxed)]">
        Choose what you’re notified about. Follow a page or space (the bell icon in
        its header) to get its updates.
      </p>

      <label className="flex items-start gap-[var(--space-3)] rounded-[var(--radius-md)] border border-[var(--border-subtle)] px-[var(--space-4)] py-[var(--space-3)]">
        <Checkbox
          checked={autowatch.data ?? true}
          disabled={autowatch.isLoading || setAutowatch.isPending}
          aria-label="Automatically follow pages I create, edit, or comment on"
          onCheckedChange={(v) => setAutowatch.mutate(v === true)}
          className="mt-[2px]"
        />
        <span className="flex flex-col gap-[1px]">
          <span className="text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium">
            Automatically follow pages I act on
          </span>
          <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">
            Follow a page when you create, edit, or comment on it, so you hear about
            later changes. Turn off to follow only manually.
          </span>
        </span>
      </label>

      {prefs.isLoading ? (
        <p className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">Loading…</p>
      ) : prefs.isError ? (
        <p role="alert" className="m-0 text-[length:var(--text-sm)] text-[var(--danger)]">
          Couldn’t load your preferences.
        </p>
      ) : (
        <div className="flex flex-col rounded-[var(--radius-md)] border border-[var(--border-subtle)] overflow-hidden">
          {/* Header row */}
          <div className="grid grid-cols-[1fr_5rem_5rem] items-center gap-[var(--space-3)] px-[var(--space-4)] py-[var(--space-2)] bg-[var(--surface-2)] border-b border-[var(--border-subtle)]">
            <span className="text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
              Event
            </span>
            {CHANNELS.map((c) => (
              <span
                key={c.channel}
                className="text-center text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
              >
                {c.label}
              </span>
            ))}
          </div>

          {events.map((ev, i) => (
            <div
              key={ev.type}
              className={cn(
                'grid grid-cols-[1fr_5rem_5rem] items-center gap-[var(--space-3)] px-[var(--space-4)] py-[var(--space-3)]',
                i > 0 && 'border-t border-[var(--border-subtle)]',
              )}
            >
              <div className="flex flex-col gap-[1px] min-w-0">
                <span className="text-[length:var(--text-sm)] text-[var(--text-primary)] font-medium">
                  {ev.label}
                </span>
                <span className="text-[length:var(--text-xs)] text-[var(--text-muted)]">
                  {ev.desc}
                </span>
              </div>
              {CHANNELS.map((c) => (
                <div key={c.channel} className="flex justify-center">
                  <Checkbox
                    checked={enabled(ev.type, c.channel)}
                    disabled={update.isPending}
                    aria-label={`${ev.label} — ${c.label}`}
                    onCheckedChange={(v) =>
                      toggle({ event_type: ev.type, channel: c.channel, enabled: v === true })
                    }
                  />
                </div>
              ))}
            </div>
          ))}
        </div>
      )}

      <p className="m-0 text-[length:var(--text-xs)] text-[var(--text-muted)]">
        Email delivery isn’t live yet — your Email choices are saved and take effect
        when it ships.
      </p>
    </section>
  )
}
