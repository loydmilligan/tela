import { forwardRef } from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { cn } from '../../lib/utils'

// eslint-disable-next-line react-refresh/only-export-components
export const Dialog = DialogPrimitive.Root
// eslint-disable-next-line react-refresh/only-export-components
export const DialogTrigger = DialogPrimitive.Trigger
// eslint-disable-next-line react-refresh/only-export-components
export const DialogClose = DialogPrimitive.Close
// eslint-disable-next-line react-refresh/only-export-components
export const DialogPortal = DialogPrimitive.Portal

export const DialogOverlay = forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(function DialogOverlay({ className, ...props }, ref) {
  return (
    <DialogPrimitive.Overlay
      ref={ref}
      className={cn('tela-dialog-overlay', className)}
      {...props}
    />
  )
})

export const DialogContent = forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & {
    showClose?: boolean
  }
>(function DialogContent(
  { className, children, showClose = true, ...props },
  ref,
) {
  return (
    <DialogPortal>
      <DialogOverlay />
      <DialogPrimitive.Content
        ref={ref}
        className={cn('tela-dialog-content', className)}
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
    </DialogPortal>
  )
})

export function DialogHeader({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn('flex flex-col gap-[var(--space-1)]', className)}
      {...props}
    />
  )
}

export function DialogFooter({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        'flex items-center justify-end gap-[var(--space-3)]',
        'pt-[var(--space-2)]',
        className,
      )}
      {...props}
    />
  )
}

export const DialogTitle = forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(function DialogTitle({ className, ...props }, ref) {
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

export const DialogDescription = forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(function DialogDescription({ className, ...props }, ref) {
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
