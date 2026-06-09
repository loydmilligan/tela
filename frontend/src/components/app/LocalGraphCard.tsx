import { Suspense, lazy } from 'react'
import { CollapsibleSection } from '../ui/collapsible-section'

const LocalGraph = lazy(() => import('./LocalGraph'))

// Placement A — the "Connections" card in the below-editor zone (next to Child
// pages / Backlinks). Collapsed by default so it's not visual noise on every
// page; the graph engine (PageGraph + d3-force) is a lazy import that only
// mounts once the section is first expanded (mountOnOpen), so a reader who
// never opens it never fetches the graph. The choice persists across pages.
export function LocalGraphCard({ pageId }: { pageId: number }) {
  return (
    <CollapsibleSection
      title="Connections"
      persistKey="tela:page-connections-open"
      mountOnOpen
    >
      <div className="h-[calc(var(--space-8)*5)] overflow-hidden rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-1)]">
        <Suspense fallback={<Skeleton />}>
          <LocalGraph pageId={pageId} depth={1} />
        </Suspense>
      </div>
    </CollapsibleSection>
  )
}

function Skeleton() {
  return <div className="h-full w-full bg-[var(--surface-2)]" aria-hidden />
}
