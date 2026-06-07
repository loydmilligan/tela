import { useMemo, useState } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Search, X } from 'lucide-react'
import {
  buildSpacePalette,
  useGraph,
  type GraphLink,
} from '../../lib/queries/graph'
import { navigateToPage } from '../../lib/pageHitItem'
import { Button } from '../ui/button'
import { PageGraph } from './PageGraph'

interface GraphSearchParams {
  focus?: number
  space?: number
}

// BFS the (undirected) graph from a seed to a hop depth — powers focus mode,
// where the graph is scoped to a page's neighborhood instead of the whole set.
function neighborhood(
  seed: number,
  links: GraphLink[],
  depth: number,
): Set<number> {
  const adj = new Map<number, number[]>()
  for (const l of links) {
    ;(adj.get(l.source) ?? adj.set(l.source, []).get(l.source)!).push(l.target)
    ;(adj.get(l.target) ?? adj.set(l.target, []).get(l.target)!).push(l.source)
  }
  const seen = new Set<number>([seed])
  let frontier = [seed]
  for (let i = 0; i < depth; i++) {
    const next: number[] = []
    for (const id of frontier) {
      for (const nb of adj.get(id) ?? []) {
        if (!seen.has(nb)) {
          seen.add(nb)
          next.push(nb)
        }
      }
    }
    frontier = next
  }
  return seen
}

export function GraphRoute() {
  const { focus, space } = useSearch({ from: '/_app/graph' }) as GraphSearchParams
  const navigate = useNavigate()
  const graph = useGraph(space)

  const [showLinks, setShowLinks] = useState(true)
  const [showTree, setShowTree] = useState(true)
  const [orphansOnly, setOrphansOnly] = useState(false)
  const [query, setQuery] = useState('')

  const allNodes = useMemo(() => graph.data?.nodes ?? [], [graph.data])
  const allLinks = useMemo(() => graph.data?.links ?? [], [graph.data])
  const palette = useMemo(() => buildSpacePalette(allNodes), [allNodes])

  // Displayed node set: focus neighborhood (2 hops) and/or orphans-only.
  const { nodes, links } = useMemo(() => {
    let keep = new Set(allNodes.map((n) => n.id))
    if (focus != null) keep = neighborhood(focus, allLinks, 2)
    if (orphansOnly) {
      const connected = new Set<number>()
      for (const l of allLinks) {
        connected.add(l.source)
        connected.add(l.target)
      }
      keep = new Set([...keep].filter((id) => !connected.has(id)))
    }
    const ns = allNodes.filter((n) => keep.has(n.id))
    const ls = allLinks.filter((l) => keep.has(l.source) && keep.has(l.target))
    return { nodes: ns, links: ls }
  }, [allNodes, allLinks, focus, orphansOnly])

  const matchedIds = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return null
    return new Set(
      nodes.filter((n) => n.title.toLowerCase().includes(q)).map((n) => n.id),
    )
  }, [nodes, query])

  const onNavigate = (id: number) => {
    const n = allNodes.find((x) => x.id === id)
    if (n) navigateToPage(n.space_id, id)
  }

  return (
    <div className="flex h-full flex-col">
      {/* Controls */}
      <div className="flex flex-wrap items-center gap-[var(--space-3)] border-b border-[var(--border-subtle)] px-[var(--space-5)] py-[var(--space-3)]">
        <h1 className="m-0 text-[length:var(--text-base)] font-medium text-[var(--text-primary)]">
          Graph
        </h1>
        {focus != null ? (
          <Button
            variant="secondary"
            size="sm"
            onClick={() => navigate({ to: '/graph', search: (s) => ({ ...s, focus: undefined }) })}
          >
            <X width={14} height={14} />
            Focused — show all
          </Button>
        ) : null}

        <div className="relative ml-auto">
          <Search
            width={14}
            height={14}
            className="pointer-events-none absolute left-[var(--space-2)] top-1/2 -translate-y-1/2 text-[var(--text-muted)]"
            aria-hidden
          />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Highlight…"
            aria-label="Highlight pages"
            className="h-[var(--space-8)] w-[12rem] rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--surface-1)] pl-[calc(var(--space-2)*2+var(--space-2))] pr-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-primary)] outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
          />
        </div>

        <ToggleChip active={showLinks} onClick={() => setShowLinks((v) => !v)}>
          Links
        </ToggleChip>
        <ToggleChip active={showTree} onClick={() => setShowTree((v) => !v)}>
          Hierarchy
        </ToggleChip>
        <ToggleChip active={orphansOnly} onClick={() => setOrphansOnly((v) => !v)}>
          Orphans
        </ToggleChip>
      </div>

      {/* Canvas + legend */}
      <div className="relative min-h-0 flex-1">
        {graph.isLoading ? (
          <CenterNote>Loading graph…</CenterNote>
        ) : nodes.length === 0 ? (
          <CenterNote>
            {orphansOnly ? 'No orphan pages — everything is linked.' : 'No pages to graph yet.'}
          </CenterNote>
        ) : (
          <PageGraph
            nodes={nodes}
            links={links}
            showLinks={showLinks}
            showTree={showTree}
            currentId={focus ?? null}
            matchedIds={matchedIds}
            onNavigate={onNavigate}
          />
        )}

        {palette.length > 1 ? (
          <div className="absolute bottom-[var(--space-3)] left-[var(--space-3)] flex max-w-[16rem] flex-col gap-[var(--space-1)] rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-1)]/90 p-[var(--space-2)] backdrop-blur">
            {palette.map((p) => (
              <div
                key={p.spaceId}
                className="flex items-center gap-[var(--space-2)] text-[length:var(--text-xs)] text-[var(--text-muted)]"
              >
                <span
                  className="inline-block h-[var(--space-2)] w-[var(--space-2)] rounded-full"
                  style={{ background: `var(${p.varName})` }}
                  aria-hidden
                />
                <span className="truncate">{p.spaceName}</span>
              </div>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  )
}

function ToggleChip({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={[
        'h-[var(--space-7)] rounded-[var(--radius-sm)] border px-[var(--space-3)] text-[length:var(--text-sm)] transition-colors',
        active
          ? 'border-[var(--accent)] bg-[color-mix(in_srgb,var(--accent)_12%,transparent)] text-[var(--text-primary)]'
          : 'border-[var(--border-subtle)] text-[var(--text-muted)] hover:text-[var(--text-primary)]',
      ].join(' ')}
    >
      {children}
    </button>
  )
}

function CenterNote({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-full items-center justify-center p-[var(--space-7)] text-center text-[length:var(--text-sm)] text-[var(--text-muted)]">
      {children}
    </div>
  )
}
