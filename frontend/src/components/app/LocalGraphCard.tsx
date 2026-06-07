import { Suspense, lazy, useEffect, useRef, useState } from 'react'

const LocalGraph = lazy(() => import('./LocalGraph'))

// Placement A — the "Connections" card in the below-editor zone (next to Child
// pages / Backlinks). It's below the fold and entirely optional, so it must not
// cost anything on page load: the graph engine (PageGraph + d3-force) is a lazy
// import, and we only mount it once the card scrolls near the viewport via an
// IntersectionObserver. A reader who never scrolls down never fetches the graph.

export function LocalGraphCard({ pageId }: { pageId: number }) {
  const ref = useRef<HTMLDivElement>(null)
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    const el = ref.current
    if (!el || visible) return
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          setVisible(true)
          io.disconnect()
        }
      },
      { rootMargin: '200px' },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [visible])

  return (
    <section
      ref={ref}
      className="flex flex-col gap-[var(--space-2)] pt-[var(--space-4)] border-t border-[var(--border-subtle)]"
    >
      <h2 className="m-0 text-[length:var(--text-sm)] font-medium text-[var(--text-muted)]">
        Connections
      </h2>
      <div className="h-[calc(var(--space-8)*5)] overflow-hidden rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-1)]">
        {visible ? (
          <Suspense fallback={<Skeleton />}>
            <LocalGraph pageId={pageId} depth={1} />
          </Suspense>
        ) : (
          <Skeleton />
        )}
      </div>
    </section>
  )
}

function Skeleton() {
  return <div className="h-full w-full bg-[var(--surface-2)]" aria-hidden />
}
