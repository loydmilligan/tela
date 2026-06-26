import { useParams } from '@tanstack/react-router'
import { useAtlasRun } from '../../../lib/queries/atlas'

// Placeholder for the run-detail screen (live stage stream, coverage, stats).
// The visual design is being locked separately — this stub only proves the
// route + data layer wire up. Reads the run from /api/atlas/runs/{id}.
export function AtlasRun() {
  const { runId } = useParams({ from: '/_app/atlas/runs/$runId' })
  const { data, isPending, isError } = useAtlasRun(runId)
  const run = data?.run

  return (
    <div className="mx-auto w-full max-w-[var(--content-max,72rem)] px-[var(--space-5)] py-[var(--space-5)]">
      <h1 className="text-[length:var(--text-2xl)] font-semibold text-[var(--text-primary)]">
        {isPending
          ? 'Loading run…'
          : isError
            ? 'Run unavailable'
            : `Run #${run?.id ?? runId}`}
      </h1>
      <p className="mt-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
        {run
          ? `Status: ${run.status} · stage: ${run.stage}`
          : 'Run screen coming — live progress, coverage and stats land here once the visual direction is locked.'}
      </p>
    </div>
  )
}
