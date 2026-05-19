import { forwardRef } from 'react'
import * as TooltipPrimitive from '@radix-ui/react-tooltip'
import { cn } from '../../lib/utils'

// eslint-disable-next-line react-refresh/only-export-components
export const TooltipProvider = TooltipPrimitive.Provider
// eslint-disable-next-line react-refresh/only-export-components
export const Tooltip = TooltipPrimitive.Root
// eslint-disable-next-line react-refresh/only-export-components
export const TooltipTrigger = TooltipPrimitive.Trigger
// eslint-disable-next-line react-refresh/only-export-components
export const TooltipPortal = TooltipPrimitive.Portal

export const TooltipContent = forwardRef<
  React.ElementRef<typeof TooltipPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>
>(function TooltipContent(
  { className, sideOffset = 6, children, ...props },
  ref,
) {
  return (
    <TooltipPortal>
      <TooltipPrimitive.Content
        ref={ref}
        sideOffset={sideOffset}
        className={cn('tela-tooltip-content', className)}
        {...props}
      >
        {children}
        <TooltipPrimitive.Arrow className="tela-tooltip-arrow" />
      </TooltipPrimitive.Content>
    </TooltipPortal>
  )
})
