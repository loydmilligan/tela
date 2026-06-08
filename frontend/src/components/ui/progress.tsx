import { forwardRef } from 'react'
import { cn } from '../../lib/utils'

// Progress — a token-only meter for usage-vs-limit bars. The fill tone escalates
// with how full it is (neutral → warning → danger) so an account nearing its
// plan limit reads at a glance. `tone="auto"` (default) derives that from
// value/max; pass an explicit tone to override. An indeterminate `max` (null)
// renders a full neutral track (used for "unlimited" plans where there's no cap
// to fill against).

type ProgressTone = 'auto' | 'neutral' | 'warning' | 'danger'

export interface ProgressProps
  extends Omit<React.HTMLAttributes<HTMLDivElement>, 'role'> {
  value: number
  /** The cap. null/undefined = unlimited → a calm, never-full track. */
  max?: number | null
  tone?: ProgressTone
}

function resolveTone(tone: ProgressTone, ratio: number): string {
  const t =
    tone === 'auto'
      ? ratio >= 1
        ? 'danger'
        : ratio >= 0.8
          ? 'warning'
          : 'neutral'
      : tone
  switch (t) {
    case 'danger':
      return 'var(--danger)'
    case 'warning':
      return 'var(--warning)'
    default:
      return 'var(--accent)'
  }
}

export const Progress = forwardRef<HTMLDivElement, ProgressProps>(
  function Progress({ value, max, tone = 'auto', className, ...props }, ref) {
    const unlimited = max == null
    const ratio = unlimited || max <= 0 ? 0 : Math.min(value / max, 1)
    const pct = unlimited ? 0 : Math.round(ratio * 100)
    return (
      <div
        ref={ref}
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={unlimited ? undefined : max}
        aria-valuenow={unlimited ? undefined : value}
        className={cn(
          'h-[var(--space-2)] w-full overflow-hidden rounded-[var(--radius-sm)] bg-[var(--surface-3)]',
          className,
        )}
        {...props}
      >
        <div
          className="h-full rounded-[var(--radius-sm)] transition-[width] duration-[var(--duration-base)] ease-[var(--ease-out)]"
          style={{
            width: unlimited ? '100%' : `${pct}%`,
            backgroundColor: unlimited
              ? 'var(--border-strong)'
              : resolveTone(tone, ratio),
            opacity: unlimited ? 0.4 : 1,
          }}
        />
      </div>
    )
  },
)
