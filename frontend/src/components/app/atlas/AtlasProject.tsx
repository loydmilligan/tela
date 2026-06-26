import { useParams } from '@tanstack/react-router'
import { useAtlasProject } from '../../../lib/queries/atlas'

// Placeholder for the per-project operator screen (sources, runs, schedule,
// output). The visual design is being locked separately — this stub only proves
// the route + data layer wire up. Reads the project from the new
// /api/atlas/projects/{id} API and renders its name.
export function AtlasProject() {
  const { projectId } = useParams({ from: '/_app/atlas/projects/$projectId' })
  const { data, isPending, isError } = useAtlasProject(projectId)
  const project = data?.project

  return (
    <div className="mx-auto w-full max-w-[var(--content-max,72rem)] px-[var(--space-5)] py-[var(--space-5)]">
      <h1 className="text-[length:var(--text-2xl)] font-semibold text-[var(--text-primary)]">
        {isPending
          ? 'Loading project…'
          : isError
            ? 'Project unavailable'
            : (project?.name ?? `Project #${projectId}`)}
      </h1>
      <p className="mt-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
        Project screen coming — sources, runs, schedule and output land here once
        the visual direction is locked.
      </p>
    </div>
  )
}
