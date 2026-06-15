import { AlertTriangle, Info } from 'lucide-react'
import { useHostContext } from '../../lib/queries/host-context'

// App-wide notice an instance admin sets (Settings → Instance) — e.g. "AI is
// paused for maintenance." Soft, non-dismissible (it reflects a live state and
// clears itself when the admin unsets it). `warning` level tints it red; `info`
// stays neutral. Shown both pre-login and in the app since it rides host-context.
export function MaintenanceBanner() {
  const m = useHostContext().data?.maintenance
  if (!m || !m.notice.trim()) return null

  const warn = m.level === 'warning'
  const Icon = warn ? AlertTriangle : Info
  return (
    <div
      role="status"
      className="flex items-center justify-center gap-[var(--space-2)] border-b px-[var(--space-4)] py-[var(--space-2)] text-[length:var(--text-sm)]"
      style={
        warn
          ? {
              borderColor: 'color-mix(in oklch, var(--danger) 25%, transparent)',
              backgroundColor: 'color-mix(in oklch, var(--danger) 10%, transparent)',
              color: 'var(--danger)',
            }
          : {
              borderColor: 'var(--border-subtle)',
              backgroundColor: 'var(--surface-2)',
              color: 'var(--text-primary)',
            }
      }
    >
      <Icon width={15} height={15} aria-hidden className="shrink-0" />
      <span className="min-w-0">{m.notice}</span>
    </div>
  )
}
