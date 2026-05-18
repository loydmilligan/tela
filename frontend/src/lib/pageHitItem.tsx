import { FileText } from 'lucide-react'
import type { CommandItem } from '../components/ui/command'
import { HighlightedSnippet } from './highlightSnippet'
import { router } from '../routes/router'

// Common shape for both tier-1 (orama TitleHit) and tier-2 (SearchResult) rows
// in the command palette. Keeping the factory's input narrow makes the call
// sites at the host explicit about which fields they're mapping.
export interface PageHit {
  pageId: number
  spaceId: number
  title: string
  breadcrumb: string[]
}

export interface PageHitItemOptions {
  // Prefix namespaces the row id so tier-1 and tier-2 hits for the same page
  // don't collide in cmdk's value space.
  idPrefix: string
  // Tier-2 only — backend-supplied snippet with literal <mark> delimiters.
  // Rendered via HighlightedSnippet (XSS-safe) when present.
  snippet?: string
}

export function navigateToPage(spaceId: number, pageId: number) {
  void router.navigate({
    to: '/spaces/$spaceId/pages/$pageId',
    params: { spaceId, pageId },
  })
}

export function pageHitToCommandItem(
  hit: PageHit,
  opts: PageHitItemOptions,
): CommandItem {
  return {
    id: `${opts.idPrefix}:${hit.pageId}`,
    title: hit.title || 'Untitled',
    subtitle:
      opts.snippet != null ? (
        <HighlightedSnippet snippet={opts.snippet} />
      ) : undefined,
    breadcrumb:
      hit.breadcrumb.length > 0 ? hit.breadcrumb.join(' / ') : undefined,
    icon: <FileText aria-hidden width={14} height={14} />,
    onSelect: () => navigateToPage(hit.spaceId, hit.pageId),
  }
}
