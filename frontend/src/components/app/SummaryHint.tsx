import { Text } from 'lucide-react'
import { cn } from '../../lib/utils'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'

/**
 * pageSummary — the standfirst a page declares in frontmatter. Same key
 * preference as the blog index excerpt and the public reader's meta
 * description (summary > excerpt > description), so every surface agrees on
 * what "the summary" is.
 */
// eslint-disable-next-line react-refresh/only-export-components
export function pageSummary(
  props?: Record<string, unknown> | null,
): string | null {
  if (!props) return null
  for (const k of ['summary', 'excerpt', 'description']) {
    const v = props[k]
    if (typeof v === 'string' && v.trim() !== '') return v.trim()
  }
  return null
}

export interface SummaryHintProps {
  summary: string
  /** Positioning within the caller's `group relative` title wrapper. */
  className?: string
}

/**
 * SummaryHint — a quiet affordance for a page's summary. An icon sits in the
 * title's left gutter, invisible until the title row is hovered (or the
 * button is focused); hovering/focusing it opens a readable summary card
 * beside the title. Callers render it only when a summary exists and place it
 * inside a `group relative` wrapper around the title.
 */
export function SummaryHint({ summary, className }: SummaryHintProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-label="Page summary"
          className={cn(
            'items-center justify-center h-[var(--space-6)] w-[var(--space-6)]',
            'rounded-[var(--radius-sm)] border-none bg-transparent p-0 cursor-default',
            'text-[var(--text-muted)] hover:text-[var(--text-primary)] hover:bg-[var(--surface-2)]',
            'opacity-0 transition-opacity duration-[var(--duration-fast)]',
            'group-hover:opacity-100 focus-visible:opacity-100',
            'data-[state=delayed-open]:opacity-100 data-[state=instant-open]:opacity-100',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]',
            className,
          )}
        >
          <Text width={14} height={14} />
        </button>
      </TooltipTrigger>
      <TooltipContent
        side="bottom"
        align="start"
        className="max-w-[24rem] p-[var(--space-3)] text-[length:var(--text-sm)] leading-[var(--leading-normal)]"
      >
        <p className="m-0 mb-[var(--space-1)] text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-mono)]">
          summary
        </p>
        <p className="m-0">{summary}</p>
      </TooltipContent>
    </Tooltip>
  )
}
