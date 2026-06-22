// Tooltip composition for the sidebar StalenessDot. The dot unions the
// background-backfill subsystems that have a freshness rollup — RAG indexing
// and summaries — so one calm marker means "this space/page has background work
// outstanding". Each subsystem is gated by its own enabled flag upstream.
//
// The dot signals *pending* backfill (the worker will catch up and it clears),
// NOT hard errors: a failed summary is deliberately excluded here so a
// permanently-failing page can't wedge the dot on (failures surface in
// Settings → Summaries instead).

// Per-space tooltip from per-subsystem backlog counts. Clauses are kept
// separate — a page can be behind on both, so summing would double-count.
// Returns null when nothing is behind.
export function spaceStaleLabel(
  indexing: number,
  summarizing: number,
): string | null {
  const parts: string[] = []
  if (indexing > 0)
    parts.push(`${indexing} ${indexing === 1 ? 'page needs' : 'pages need'} indexing`)
  if (summarizing > 0)
    parts.push(
      `${summarizing} ${summarizing === 1 ? 'page needs' : 'pages need'} summarizing`,
    )
  return parts.length ? parts.join(' · ') : null
}

// Per-page tooltip: which backfills this one page is behind on. null = none.
export function pageStaleLabel(
  indexing: 'stale' | 'unindexed' | null,
  summarizing: 'stale' | 'missing' | null,
): string | null {
  const parts: string[] = []
  if (indexing === 'stale') parts.push('Edited since last indexed')
  else if (indexing === 'unindexed') parts.push('Not indexed yet')
  if (summarizing === 'stale') parts.push('Summary out of date')
  else if (summarizing === 'missing') parts.push('Not summarized yet')
  return parts.length ? parts.join(' · ') : null
}
