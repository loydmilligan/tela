import { useEffect } from 'react'
import { useParams } from '@tanstack/react-router'
import { usePublicSpace, usePublicSpacePage } from '../lib/queries/public'
import { pageSlug } from '../lib/slug'
import { PublicReaderView } from '../components/app/PublicReader'
import { ThemeSwitcher } from '../components/ThemeSwitcher'
import { Card, CardDescription, CardHeader, CardTitle } from '../components/ui/card'

// Public-space reader route — child of `rootRoute` (NOT appLayoutRoute) because
// it's unauthenticated: a logged-out reader views a public space here. Data
// comes from the /api/public/ endpoints (raw fetch, never `api()`), so a miss
// is a plain 404, not a bounce to /login.

function PublicShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-dvh flex flex-col bg-[var(--surface-1)] text-[var(--text-primary)]">
      <header className="flex items-center justify-between px-[var(--space-6)] py-[var(--space-3)] border-b border-[var(--border-subtle)] shrink-0">
        <h1 className="m-0 text-[length:var(--text-lg)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)]">
          <a
            href="/"
            aria-label="tela home"
            className="inline-block rounded-[var(--radius-xs)] text-[var(--text-primary)] no-underline transition-opacity duration-[var(--duration-fast)] hover:opacity-70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
          >
            tela
          </a>
        </h1>
        <ThemeSwitcher />
      </header>
      <main className="flex-1 flex items-center justify-center p-[var(--space-7)]">
        {children}
      </main>
    </div>
  )
}

function PublicUnavailable({
  message = 'This page is not publicly available.',
}: {
  message?: string
}) {
  return (
    <PublicShell>
      <Card className="w-full max-w-[24rem]">
        <CardHeader>
          <CardTitle className="text-[length:var(--text-2xl)]">Not available</CardTitle>
          <CardDescription>{message}</CardDescription>
        </CardHeader>
      </Card>
    </PublicShell>
  )
}

export function PublicReaderRoute() {
  const { spaceId, pageId } = useParams({
    from: '/public/spaces/$spaceId/pages/$pageId/{-$slug}',
  })
  const spaceQuery = usePublicSpace(spaceId)
  const pageQuery = usePublicSpacePage(spaceId, pageId, spaceQuery.data != null)

  // Canonicalise the address to /.../{slug} once the title is known. replaceState
  // (not router nav) so a slug change never remounts the reader.
  const pageTitle = pageQuery.data?.page.title ?? ''
  useEffect(() => {
    if (!pageQuery.data) return
    const slug = pageSlug(pageTitle)
    const base = `/public/spaces/${spaceId}/pages/${pageId}`
    const desired = slug ? `${base}/${slug}` : base
    if (window.location.pathname !== desired) {
      window.history.replaceState(
        window.history.state,
        '',
        desired + window.location.search + window.location.hash,
      )
    }
  }, [pageQuery.data, pageTitle, spaceId, pageId])

  if (spaceQuery.isLoading || (spaceQuery.data && pageQuery.isLoading)) {
    return (
      <PublicShell>
        <p role="status" className="m-0 text-[length:var(--text-sm)] text-[var(--text-muted)]">
          Loading…
        </p>
      </PublicShell>
    )
  }

  // Either the space isn't public, or the page isn't in it → one neutral 404
  // surface (never confirm a private space/page exists).
  if (spaceQuery.error || !spaceQuery.data || pageQuery.error || !pageQuery.data) {
    return <PublicUnavailable />
  }

  return (
    <PublicReaderView
      space={spaceQuery.data.space}
      pageId={pageQuery.data.page.id}
      pageTitle={pageQuery.data.page.title}
      pageBody={pageQuery.data.page.body}
      pageProps={pageQuery.data.page.props}
      updatedAt={pageQuery.data.page.updated_at}
    />
  )
}
