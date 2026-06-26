// Atlas — the doc-generation operator surface. This is the placeholder shell;
// the product-grade Home (projects grouped per person/org, status cards, the
// New-project flow) is built on top of the new /api/atlas/projects API.
export function AtlasHome() {
  return (
    <div className="mx-auto w-full max-w-[var(--content-max,72rem)] px-[var(--space-5)] py-[var(--space-5)]">
      <h1 className="text-[length:var(--text-2xl)] font-semibold text-[var(--text-primary)]">
        Atlas
      </h1>
      <p className="mt-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-muted)]">
        Generate coverage-audited documentation from your git repos and Jira projects.
      </p>
    </div>
  )
}
