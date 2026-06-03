import { forwardRef } from 'react'
import { Globe, KeyRound, Users, type LucideIcon } from 'lucide-react'
import { Badge } from './badge'
import { cn } from '../../lib/utils'
import type { ExposureState } from '../../lib/types'

// VisibilityBadge — the one ambient indicator of a page's public-link exposure
// (Axis 2 in docs/visibility-model.md). Internal "who can read this" is the
// space's membership and is shown elsewhere; this chip answers only "is it
// exposed outside the space, and how". Lucide icons, token-only styling.
//
//   private  → Users  "Space"     (muted)  — members only, the resting state
//   public   → Globe  "Public"    (accent) — open link, no password
//   password → KeyRound "Password" (accent) — link + password
//
// Exposed states use the accent treatment deliberately: exposure is the thing a
// glance should catch. Inherited exposure (from an ancestor's include-children
// share) reads quieter — muted — since it isn't set on this page.
//
// `compact` renders a bare tinted icon (no chip chrome) for dense surfaces like
// the sidebar tree; the full form renders the owned Badge chip with a label.

const CONFIG: Record<
  ExposureState,
  { icon: LucideIcon; label: string; variant: 'muted' | 'accent' }
> = {
  private: { icon: Users, label: 'Space', variant: 'muted' },
  public: { icon: Globe, label: 'Public', variant: 'accent' },
  password: { icon: KeyRound, label: 'Password', variant: 'accent' },
}

export interface VisibilityBadgeProps
  extends React.HTMLAttributes<HTMLSpanElement> {
  state: ExposureState
  /** exposure comes only from an ancestor's include-descendants share */
  inherited?: boolean
  /** icon-only, for dense surfaces like the sidebar tree */
  compact?: boolean
}

export const VisibilityBadge = forwardRef<
  HTMLSpanElement,
  VisibilityBadgeProps
>(function VisibilityBadge(
  { state, inherited = false, compact = false, className, ...props },
  ref,
) {
  const { icon: Icon, label, variant } = CONFIG[state]
  const text = inherited ? `${label} · inherited` : label
  // Inherited (and the resting private state) read muted; live exposure pops.
  const muted = inherited || state === 'private'

  if (compact) {
    return (
      <span
        ref={ref}
        role="img"
        aria-label={text}
        title={text}
        className={cn(
          'inline-flex items-center shrink-0',
          muted ? 'text-[var(--text-muted)]' : 'text-[var(--accent)]',
          className,
        )}
        {...props}
      >
        <Icon width={13} height={13} aria-hidden />
      </span>
    )
  }

  return (
    <Badge
      ref={ref}
      variant={muted ? 'muted' : variant}
      className={cn(inherited && 'opacity-80', className)}
      {...props}
    >
      <Icon width={14} height={14} aria-hidden />
      <span>{text}</span>
    </Badge>
  )
})
