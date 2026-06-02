import { useCallback } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useShareTree, type SharePublicMeta } from '../../lib/queries/share'
import { Button } from '../ui/button'
import { ReaderShell } from './ReaderShell'
import { ShareSidebar } from './ShareLayout'

interface ShareReaderViewProps {
  token: string
  share: SharePublicMeta
  pageId: number
  pageTitle: string
  pageBody: string
  updatedAt: string
  /** In-scope page ids — drives wikilink scoping and the subtree sidebar. */
  inScopePageIds: Set<number>
}

// Public share view, rendered through the shared reading-mode shell. Same
// chrome-free reader as authenticated /read (TOC, typeface/size/theme controls,
// print-to-PDF), with share-specific wiring: the top-bar leading slot is the
// tela wordmark instead of a close button, the trailing slot offers Sign in,
// wikilinks stay inside the share (in-scope hop, out-of-scope no-op), and a
// multi-page share gets the in-scope subtree as the reader's left rail.
export function ShareReaderView({
  token,
  share,
  pageId,
  pageTitle,
  pageBody,
  updatedAt,
  inScopePageIds,
}: ShareReaderViewProps) {
  const navigate = useNavigate()

  // Subtree nav — only fetched/shown for descendant-inclusive shares with more
  // than the root page. Same gating ShareLayout used.
  const treeEnabled = share.include_descendants
  const tree = useShareTree(token, treeEnabled)
  const pages = tree.data?.pages ?? []
  const showSidebar = treeEnabled && pages.length > 1

  const onNavigateWikilink = useCallback(
    (targetPageId: number) => {
      // In-scope links route within share-mode; out-of-scope ones are painted
      // as plain text by the decoration plugin and stay non-navigable here.
      if (!inScopePageIds.has(targetPageId)) return
      void navigate({
        to: '/share/$token/p/$pageId',
        params: { token, pageId: targetPageId },
      })
    },
    [navigate, token, inScopePageIds],
  )

  return (
    <ReaderShell
      pageId={pageId}
      title={pageTitle}
      body={pageBody}
      updatedAt={updatedAt}
      wikilinkMode="share"
      aliveWikilinkIds={inScopePageIds}
      onNavigateWikilink={onNavigateWikilink}
      sidebar={showSidebar ? <ShareSidebar token={token} pages={pages} /> : undefined}
      topbarLeading={
        <span className="font-[family-name:var(--font-sans)] text-[length:var(--text-base)] font-medium text-[var(--text-primary)]">
          tela
        </span>
      }
      topbarTrailing={
        /* Plain <a> rather than the router Link: a full page reload on sign-in
           is intentional so the post-login app boots cleanly outside the
           share-mode shell. */
        <Button asChild variant="ghost" size="sm">
          <a href="/login">Sign in</a>
        </Button>
      }
    />
  )
}
