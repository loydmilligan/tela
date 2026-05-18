import { forwardRef } from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '../../lib/utils'

const textareaVariants = cva(
  [
    'block w-full',
    'leading-[var(--leading-relaxed)]',
    'rounded-[var(--radius-md)]',
    'bg-[var(--surface-1)] text-[var(--text-primary)]',
    'border border-[var(--border-subtle)]',
    'placeholder:text-[var(--text-muted)]',
    'transition-[background-color,color,border-color,box-shadow] duration-[var(--duration-fast)] ease-[var(--ease-out)]',
    'outline-none resize-y',
    'focus-visible:border-[var(--accent)] focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-1 focus-visible:ring-offset-[var(--surface-1)]',
    'disabled:cursor-not-allowed disabled:opacity-50 disabled:bg-[var(--surface-2)]',
    'aria-invalid:border-[var(--danger)] aria-invalid:focus-visible:ring-[var(--danger)] aria-invalid:focus-visible:border-[var(--danger)]',
  ],
  {
    variants: {
      font: {
        sans: 'font-[family-name:var(--font-sans)]',
        mono: 'font-[family-name:var(--font-mono)]',
      },
      size: {
        sm: 'text-[length:var(--text-sm)] px-[var(--space-3)] py-[var(--space-2)] min-h-[calc(var(--space-8)*2)]',
        md: 'text-[length:var(--text-base)] px-[var(--space-4)] py-[var(--space-3)] min-h-[calc(var(--space-8)*4)]',
        lg: 'text-[length:var(--text-base)] px-[var(--space-5)] py-[var(--space-4)] min-h-[calc(var(--space-8)*6)]',
      },
    },
    defaultVariants: {
      font: 'mono',
      size: 'md',
    },
  },
)

export interface TextAreaProps
  extends Omit<React.TextareaHTMLAttributes<HTMLTextAreaElement>, 'size'>,
    VariantProps<typeof textareaVariants> {}

export const TextArea = forwardRef<HTMLTextAreaElement, TextAreaProps>(
  function TextArea({ className, font, size, ...props }, ref) {
    return (
      <textarea
        ref={ref}
        className={cn(textareaVariants({ font, size }), className)}
        {...props}
      />
    )
  },
)

export { textareaVariants }
