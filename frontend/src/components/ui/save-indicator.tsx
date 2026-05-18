import { CheckCircle2, CircleAlert, Loader2 } from 'lucide-react'
import { cn } from '../../lib/utils'

export type SaveStatus = 'idle' | 'saving' | 'saved' | 'error'

interface SaveIndicatorProps {
  status: SaveStatus
  className?: string
}

// Small status badge for the page-view header. Color tokens:
//   saving → --text-muted  (in-flight, non-alarming)
//   saved  → --accent      (positive confirmation; matches theme accent)
//   error  → --danger      (failed)
//   idle   → renders nothing
export function SaveIndicator({ status, className }: SaveIndicatorProps) {
  if (status === 'idle') return null

  const map = {
    saving: {
      Icon: Loader2,
      label: 'Saving…',
      color: 'text-[var(--text-muted)]',
      spin: true,
    },
    saved: {
      Icon: CheckCircle2,
      label: 'Saved',
      color: 'text-[var(--accent)]',
      spin: false,
    },
    error: {
      Icon: CircleAlert,
      label: 'Save failed',
      color: 'text-[var(--danger)]',
      spin: false,
    },
  } as const

  const { Icon, label, color, spin } = map[status]

  return (
    <span
      role="status"
      aria-live="polite"
      className={cn(
        'inline-flex items-center gap-[var(--space-2)]',
        'text-[length:var(--text-xs)] leading-[var(--leading-tight)]',
        'font-[family-name:var(--font-sans)]',
        color,
        className,
      )}
    >
      <Icon
        aria-hidden
        width={14}
        height={14}
        className={spin ? 'animate-spin' : undefined}
      />
      {label}
    </span>
  )
}
