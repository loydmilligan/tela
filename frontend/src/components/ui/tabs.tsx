import { forwardRef } from 'react'
import * as TabsPrimitive from '@radix-ui/react-tabs'
import { cn } from '../../lib/utils'

// Owned, shadcn-style Tabs over Radix. Underline variant: a horizontal list of
// triggers with the active one marked by an accent underline. Tokens only —
// no hardcoded color/px/radii.

// eslint-disable-next-line react-refresh/only-export-components
export const Tabs = TabsPrimitive.Root

export const TabsList = forwardRef<
  React.ElementRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>
>(function TabsList({ className, ...props }, ref) {
  return (
    <TabsPrimitive.List
      ref={ref}
      className={cn(
        'flex items-stretch gap-[var(--space-1)]',
        'border-b border-[var(--border-subtle)]',
        'overflow-x-auto',
        className,
      )}
      {...props}
    />
  )
})

export const TabsTrigger = forwardRef<
  React.ElementRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>
>(function TabsTrigger({ className, ...props }, ref) {
  return (
    <TabsPrimitive.Trigger
      ref={ref}
      className={cn(
        'relative inline-flex items-center gap-[var(--space-2)] whitespace-nowrap',
        'px-[var(--space-3)] py-[var(--space-2)]',
        'font-[family-name:var(--font-sans)] text-[length:var(--text-sm)]',
        'text-[var(--text-muted)] hover:text-[var(--text-primary)]',
        'border-b-2 border-transparent -mb-px',
        'transition-colors cursor-pointer',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)] rounded-t-[var(--radius-sm)]',
        'disabled:cursor-not-allowed disabled:opacity-50 disabled:pointer-events-none',
        'data-[state=active]:text-[var(--text-primary)] data-[state=active]:font-medium',
        'data-[state=active]:border-[var(--accent)]',
        className,
      )}
      {...props}
    />
  )
})

export const TabsContent = forwardRef<
  React.ElementRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(function TabsContent({ className, ...props }, ref) {
  return (
    <TabsPrimitive.Content
      ref={ref}
      className={cn(
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)] rounded-[var(--radius-sm)]',
        className,
      )}
      {...props}
    />
  )
})
