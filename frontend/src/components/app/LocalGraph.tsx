import { useMemo } from 'react'
import { Link } from '@tanstack/react-router'
import { Maximize2 } from 'lucide-react'
import { neighborhood, useGraph } from '../../lib/queries/graph'
import { navigateToPage } from '../../lib/pageHitItem'
import { PageGraph } from './PageGraph'

// The on-page local graph: the current page + its neighborhood, scoped from the
// full /api/graph result. Used by both the below-editor "Connections" card and
// the right-rail panel. This module is the lazy-load boundary — importing it
// pulls in PageGraph + d3-force, so neither placement loads the graph engine
// until it's actually shown (card scrolled into view / panel opened).

export interface LocalGraphProps {
  pageId: number
  // Hop radius around the page. 1 = direct neighbors (compact card), 2 = a
  // wider neighborhood (the roomier panel).
  depth?: number
}

export default function LocalGraph({ pageId, depth = 1 }: LocalGraphProps) {
  const graph = useGraph()

  const { nodes, links, total } = useMemo(() => {
    const all = graph.data
    if (!all) return { nodes: [], links: [], total: 0 }
    const keep = neighborhood(pageId, all.links, depth)
    const ns = all.nodes.filter((n) => keep.has(n.id))
    const ls = all.links.filter((l) => keep.has(l.source) && keep.has(l.target))
    return { nodes: ns, links: ls, total: ns.length }
  }, [graph.data, pageId, depth])

  if (graph.isLoading) {
    return <Note>Loading…</Note>
  }
  if (total <= 1) {
    return <Note>No links to or from this page yet.</Note>
  }

  return (
    <div className="relative h-full w-full">
      <PageGraph
        nodes={nodes}
        links={links}
        showLinks
        showTree
        recency={false}
        currentId={pageId}
        onNavigate={(id) => {
          const n = nodes.find((x) => x.id === id)
          if (n) navigateToPage(n.space_id, id)
        }}
        fitSignal={`local:${pageId}`}
      />
      <Link
        to="/graph"
        search={{ focus: pageId }}
        aria-label="Open in full graph"
        title="Open in full graph"
        className="absolute right-[var(--space-2)] top-[var(--space-2)] inline-flex h-[var(--space-7)] w-[var(--space-7)] items-center justify-center rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[color-mix(in_srgb,var(--surface-1)_85%,transparent)] text-[var(--text-muted)] backdrop-blur hover:text-[var(--text-primary)]"
      >
        <Maximize2 width={14} height={14} />
      </Link>
    </div>
  )
}

function Note({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full items-center justify-center p-[var(--space-4)] text-center text-[length:var(--text-sm)] text-[var(--text-muted)]">
      {children}
    </div>
  )
}
