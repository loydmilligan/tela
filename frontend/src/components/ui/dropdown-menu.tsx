import { forwardRef } from 'react'
import * as DropdownMenuPrimitive from '@radix-ui/react-dropdown-menu'
import { cn } from '../../lib/utils'

export const DropdownMenu = DropdownMenuPrimitive.Root
export const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger
export const DropdownMenuPortal = DropdownMenuPrimitive.Portal

export const DropdownMenuContent = forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Content>
>(function DropdownMenuContent(
  { className, sideOffset = 4, align = 'start', ...props },
  ref,
) {
  return (
    <DropdownMenuPortal>
      <DropdownMenuPrimitive.Content
        ref={ref}
        sideOffset={sideOffset}
        align={align}
        className={cn('tela-dropdown-content', className)}
        {...props}
      />
    </DropdownMenuPortal>
  )
})

export interface DropdownMenuItemProps
  extends React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Item> {
  destructive?: boolean
}

export const DropdownMenuItem = forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Item>,
  DropdownMenuItemProps
>(function DropdownMenuItem({ className, destructive, ...props }, ref) {
  return (
    <DropdownMenuPrimitive.Item
      ref={ref}
      className={cn(
        'tela-dropdown-item',
        destructive && 'tela-dropdown-item-danger',
        className,
      )}
      {...props}
    />
  )
})

export const DropdownMenuSeparator = forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Separator>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Separator>
>(function DropdownMenuSeparator({ className, ...props }, ref) {
  return (
    <DropdownMenuPrimitive.Separator
      ref={ref}
      className={cn('tela-dropdown-separator', className)}
      {...props}
    />
  )
})

export const DropdownMenuLabel = forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Label>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Label>
>(function DropdownMenuLabel({ className, ...props }, ref) {
  return (
    <DropdownMenuPrimitive.Label
      ref={ref}
      className={cn('tela-dropdown-label', className)}
      {...props}
    />
  )
})
