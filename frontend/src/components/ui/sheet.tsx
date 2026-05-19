import { forwardRef } from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { type VariantProps } from 'class-variance-authority'
import { X } from 'lucide-react'
import { cn } from '../../lib/utils'
import { sheetVariants } from './sheet-variants'

// eslint-disable-next-line react-refresh/only-export-components
export const Sheet = DialogPrimitive.Root
// eslint-disable-next-line react-refresh/only-export-components
export const SheetTrigger = DialogPrimitive.Trigger
// eslint-disable-next-line react-refresh/only-export-components
export const SheetClose = DialogPrimitive.Close
// eslint-disable-next-line react-refresh/only-export-components
export const SheetPortal = DialogPrimitive.Portal

export const SheetOverlay = forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(function SheetOverlay({ className, ...props }, ref) {
  return (
    <DialogPrimitive.Overlay
      ref={ref}
      className={cn('tela-sheet-overlay', className)}
      {...props}
    />
  )
})

export interface SheetContentProps
  extends React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>,
    VariantProps<typeof sheetVariants> {
  showClose?: boolean
}

export const SheetContent = forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  SheetContentProps
>(function SheetContent(
  { side = 'right', className, children, showClose = true, ...props },
  ref,
) {
  return (
    <SheetPortal>
      <SheetOverlay />
      <DialogPrimitive.Content
        ref={ref}
        className={cn(sheetVariants({ side }), className)}
        {...props}
      >
        {children}
        {showClose ? (
          <DialogPrimitive.Close
            aria-label="Close"
            className={cn(
              'absolute top-[var(--space-3)] right-[var(--space-3)]',
              'inline-flex items-center justify-center',
              'h-[var(--space-7)] w-[var(--space-7)]',
              'rounded-[var(--radius-sm)]',
              'text-[var(--text-muted)]',
              'cursor-pointer bg-transparent border-0',
              'transition-[background-color,color] duration-[var(--duration-fast)] ease-[var(--ease-out)]',
              'hover:bg-[var(--surface-2)] hover:text-[var(--text-primary)]',
              'outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]',
            )}
          >
            <X aria-hidden width={16} height={16} />
          </DialogPrimitive.Close>
        ) : null}
      </DialogPrimitive.Content>
    </SheetPortal>
  )
})

export function SheetHeader({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        'flex flex-col gap-[var(--space-1)]',
        'px-[var(--space-6)] pt-[var(--space-6)] pb-[var(--space-4)]',
        'border-b border-[var(--border-subtle)]',
        className,
      )}
      {...props}
    />
  )
}

export function SheetBody({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        'flex-1 min-h-0 overflow-y-auto',
        'px-[var(--space-6)] py-[var(--space-4)]',
        className,
      )}
      {...props}
    />
  )
}

export function SheetFooter({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        'flex items-center justify-end gap-[var(--space-3)]',
        'px-[var(--space-6)] py-[var(--space-4)]',
        'border-t border-[var(--border-subtle)]',
        className,
      )}
      {...props}
    />
  )
}

export const SheetTitle = forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(function SheetTitle({ className, ...props }, ref) {
  return (
    <DialogPrimitive.Title
      ref={ref}
      className={cn(
        'm-0 font-[family-name:var(--font-sans)]',
        'text-[length:var(--text-xl)] leading-[var(--leading-tight)]',
        'text-[var(--text-primary)]',
        className,
      )}
      {...props}
    />
  )
})

export const SheetDescription = forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(function SheetDescription({ className, ...props }, ref) {
  return (
    <DialogPrimitive.Description
      ref={ref}
      className={cn(
        'm-0 font-[family-name:var(--font-sans)]',
        'text-[length:var(--text-sm)] leading-[var(--leading-relaxed)]',
        'text-[var(--text-muted)]',
        className,
      )}
      {...props}
    />
  )
})
