import { cn } from '../../lib/utils'

// formatScalar renders a single non-container value for display.
function formatScalar(v: unknown): string {
  if (typeof v === 'boolean') return v ? 'true' : 'false'
  return String(v)
}

// formatPropValue flattens any frontmatter value to a readable string. Scalars
// pass through; arrays join with commas; objects fall back to compact JSON.
// (Read-only first cut — complex-value editing is deferred.)
function formatPropValue(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (Array.isArray(v)) return v.map(formatScalar).join(', ')
  if (typeof v === 'object') return JSON.stringify(v)
  return formatScalar(v)
}

export interface PagePropertiesProps {
  /** The page's free-form props bag (frontmatter). Absent/empty → renders nothing. */
  props?: Record<string, unknown> | null
  className?: string
}

/**
 * PageProperties — read-only display of a page's frontmatter properties, shown
 * between the title and the editor body (Notion/Obsidian placement). Renders
 * nothing when there are no properties, so pages without frontmatter are
 * unaffected. Editing is a deliberate follow-up.
 */
export function PageProperties({ props, className }: PagePropertiesProps) {
  const entries = props ? Object.entries(props) : []
  if (entries.length === 0) return null
  return (
    <section
      aria-label="Page properties"
      className={cn(
        'flex flex-col gap-[var(--space-2)]',
        'rounded-[var(--radius-md)] border border-[var(--border-subtle)]',
        'bg-[var(--surface-1)]',
        'px-[var(--space-4)] py-[var(--space-3)]',
        className,
      )}
    >
      <h2 className="m-0 text-[length:var(--text-xs)] uppercase tracking-wider text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
        Properties
      </h2>
      <dl className="m-0 grid grid-cols-[minmax(0,10rem)_1fr] gap-x-[var(--space-4)] gap-y-[var(--space-1)]">
        {entries.map(([key, value]) => (
          <div key={key} className="contents">
            <dt className="truncate text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-mono)]">
              {key}
            </dt>
            <dd className="m-0 min-w-0 break-words text-[length:var(--text-sm)] text-[var(--text-primary)] font-[family-name:var(--font-sans)]">
              {formatPropValue(value)}
            </dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
