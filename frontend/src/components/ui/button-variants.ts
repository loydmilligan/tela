import { cva } from 'class-variance-authority'

export const buttonVariants = cva(
  [
    'inline-flex items-center justify-center gap-[var(--space-2)]',
    'font-[family-name:var(--font-sans)]',
    'leading-[var(--leading-tight)]',
    'rounded-[var(--radius-md)]',
    'border border-transparent',
    'cursor-pointer select-none whitespace-nowrap',
    'transition-[background-color,color,border-color,box-shadow,filter] duration-[var(--duration-fast)] ease-[var(--ease-out)]',
    'outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--surface-1)]',
    'disabled:cursor-not-allowed disabled:opacity-50 disabled:pointer-events-none',
  ],
  {
    variants: {
      variant: {
        primary: [
          'bg-[var(--accent)] text-[var(--accent-fg)]',
          'hover:brightness-110 active:brightness-95',
        ],
        secondary: [
          'bg-[var(--surface-2)] text-[var(--text-primary)]',
          'border-[var(--border-subtle)]',
          'hover:bg-[var(--surface-3)]',
        ],
        ghost: [
          'bg-transparent text-[var(--text-primary)]',
          'hover:bg-[var(--surface-2)]',
        ],
        danger: [
          'bg-[var(--danger)] text-[var(--text-inverse)]',
          'hover:brightness-110 active:brightness-95',
        ],
      },
      size: {
        sm: 'text-[length:var(--text-xs)] px-[var(--space-3)] py-[var(--space-1)] h-[calc(var(--space-7)-var(--space-1))]',
        md: 'text-[length:var(--text-sm)] px-[var(--space-4)] py-[var(--space-2)] h-[var(--space-8)]',
        lg: 'text-[length:var(--text-base)] px-[var(--space-5)] py-[var(--space-3)] h-[calc(var(--space-8)+var(--space-2))]',
      },
    },
    defaultVariants: {
      variant: 'primary',
      size: 'md',
    },
  },
)
