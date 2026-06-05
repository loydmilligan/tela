import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'

// A small amber dot signalling that something's index is out of date (a space
// with stale pages, or a single stale/unindexed page). Non-intrusive: just a
// dot with a tooltip; render it only when there's actually something stale.
export function StalenessDot({
  label,
  side = 'right',
}: {
  label: string
  side?: 'top' | 'right' | 'bottom' | 'left'
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          tabIndex={0}
          aria-label={label}
          className="shrink-0 inline-flex items-center justify-center cursor-default outline-none rounded-[var(--radius-xs)] focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
        >
          <span
            aria-hidden
            className="block w-[var(--space-2)] h-[var(--space-2)] rounded-full"
            style={{ backgroundColor: 'var(--warning)' }}
          />
        </span>
      </TooltipTrigger>
      <TooltipContent side={side}>{label}</TooltipContent>
    </Tooltip>
  )
}
