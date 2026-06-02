import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { ReaderShell } from '../components/app/ReaderShell'

// PDF print surface (#3). gotenberg's headless Chromium loads /print/<token>;
// this route fetches the one page the signed token authorizes and renders it
// through the shared ReaderShell — the SAME reader a human sees — so the PDF is
// pixel-identical. Public (no auth gate, like /share); the token is the
// authorization. Only ever loaded by the renderer, so the (print-hidden) topbar
// chrome is irrelevant. Raw fetch (not api()) so a 401 never bounces to /login.

interface PrintPage {
  id: number
  title: string
  body: string
  updated_at: string
  source_url: string
}

async function fetchPrintPage(token: string): Promise<PrintPage> {
  const res = await fetch(`/api/print/${encodeURIComponent(token)}`)
  if (!res.ok) throw new Error(`print ${res.status}`)
  const { page } = (await res.json()) as { page: PrintPage }
  return page
}

export function PrintRoute() {
  const { token } = useParams({ from: '/print/$token' })
  const query = useQuery({
    queryKey: ['print', token],
    queryFn: () => fetchPrintPage(token),
    retry: false,
    staleTime: Infinity,
  })

  // No wikilink targets are in scope for a single-page export — render them as
  // plain text (share mode + empty alive set) and never navigate.
  const noIds = useMemo(() => new Set<number>(), [])

  if (query.isLoading || !query.data) {
    return <div className="tela-reader" aria-busy="true" />
  }
  const page = query.data

  return (
    <ReaderShell
      pageId={page.id}
      title={page.title}
      body={page.body}
      updatedAt={page.updated_at}
      wikilinkMode="share"
      aliveWikilinkIds={noIds}
      onNavigateWikilink={() => {}}
      sourceLabel={page.source_url.replace(/^https?:\/\//, '')}
    />
  )
}
