import { forwardRef } from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '../../lib/utils'

const badgeVariants = cva(
  [
    'inline-flex items-center gap-[var(--space-1)]',
    'font-[family-name:var(--font-sans)]',
    'leading-[var(--leading-tight)]',
    'rounded-[var(--radius-sm)]',
    'border',
    'whitespace-nowrap',
    'text-[length:var(--text-xs)]',
    'px-[var(--space-2)] py-[1px]',
  ],
  {
    variants: {
      variant: {
        // Subdued chip — same surface tone as the rest of the row, just
        // outlined. Use for neutral labels like 'This device' or a
        // space-name disambiguator.
        muted: [
          'bg-[var(--surface-1)] text-[var(--text-muted)]',
          'border-[var(--border-subtle)]',
        ],
        // Brand-tinted chip — uses accent for the foreground/border with a
        // soft surface fill so it stays legible without shouting. Use when
        // the chip needs to read as "this is the active / current one".
        accent: [
          'bg-[var(--surface-2)] text-[var(--accent)]',
          'border-[var(--accent)]',
        ],
      },
    },
    defaultVariants: {
      variant: 'muted',
    },
  },
)

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

export const Badge = forwardRef<HTMLSpanElement, BadgeProps>(function Badge(
  { className, variant, ...props },
  ref,
) {
  return (
    <span
      ref={ref}
      className={cn(badgeVariants({ variant }), className)}
      {...props}
    />
  )
})
