import { forwardRef } from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '../../lib/utils'

// Avatar — owned, token-driven, Q8-rule primitive. Used by the M7.4 presence
// stack (and any future identity chip). The `tone` enum is intentionally
// closed: the 8 `collab-*` tones map 1:1 to the --collab-cursor-N tokens so a
// peer's identity colour is stable across themes and across the codebase
// (avatar background and remote cursor caret are guaranteed to match).
const avatarVariants = cva(
  [
    'inline-flex items-center justify-center',
    'rounded-full',
    'font-[family-name:var(--font-sans)]',
    'leading-none',
    'select-none',
    'uppercase',
    'overflow-hidden',
    // Centered initial — keep monospace-ish via tabular-nums so character
    // metrics don't shift the glyph in the circle.
    '[font-variant-numeric:tabular-nums]',
  ],
  {
    variants: {
      size: {
        sm: 'h-[var(--space-6)] w-[var(--space-6)] text-[length:var(--text-xs)] font-medium',
        md: 'h-[var(--space-8)] w-[var(--space-8)] text-[length:var(--text-sm)] font-medium',
        lg: 'h-[calc(var(--space-8)+var(--space-2))] w-[calc(var(--space-8)+var(--space-2))] text-[length:var(--text-base)] font-semibold',
      },
      tone: {
        neutral:
          'bg-[var(--surface-3)] text-[var(--text-muted)] border border-[var(--border-subtle)]',
        'collab-1': 'bg-[var(--collab-cursor-1)] text-[var(--text-inverse)]',
        'collab-2': 'bg-[var(--collab-cursor-2)] text-[var(--text-inverse)]',
        'collab-3': 'bg-[var(--collab-cursor-3)] text-[var(--text-inverse)]',
        'collab-4': 'bg-[var(--collab-cursor-4)] text-[var(--text-inverse)]',
        'collab-5': 'bg-[var(--collab-cursor-5)] text-[var(--text-inverse)]',
        'collab-6': 'bg-[var(--collab-cursor-6)] text-[var(--text-inverse)]',
        'collab-7': 'bg-[var(--collab-cursor-7)] text-[var(--text-inverse)]',
        'collab-8': 'bg-[var(--collab-cursor-8)] text-[var(--text-inverse)]',
      },
    },
    defaultVariants: {
      size: 'md',
      tone: 'neutral',
    },
  },
)

export type AvatarTone = NonNullable<
  VariantProps<typeof avatarVariants>['tone']
>

export interface AvatarProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof avatarVariants> {}

export const Avatar = forwardRef<HTMLSpanElement, AvatarProps>(function Avatar(
  { className, size, tone, children, ...props },
  ref,
) {
  return (
    <span
      ref={ref}
      className={cn(avatarVariants({ size, tone }), className)}
      {...props}
    >
      {children}
    </span>
  )
})
