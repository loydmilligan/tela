import { Suspense, lazy, useEffect, useMemo, useRef } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { cn } from '../../lib/utils'
import { parseWikilinkPageId } from './milkdown-wikilink-decoration'

// Lazy-loaded — share-mode bundle should not bloat the main chunk for logged-in
// users; MilkdownEditor's whole grammar/Yjs blob ships as the existing milkdown
// chunk and only loads when someone actually opens a share link.
const MilkdownEditor = lazy(() =>
  import('./milkdown-editor').then((m) => ({ default: m.MilkdownEditor })),
)

const EDITOR_MIN_H = 'min-h-[calc(var(--space-8)*8)]'

function EditorFallback() {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label="Loading reader"
      className={cn(
        EDITOR_MIN_H,
        'p-[var(--space-2)]',
        'rounded-[var(--radius-sm)]',
        'bg-[var(--surface-2)]',
      )}
    />
  )
}

interface ShareReaderProps {
  token: string
  pageTitle: string
  pageBody: string
  pageId: number
  inScopePageIds: Set<number>
}

export function ShareReader({
  token,
  pageTitle,
  pageBody,
  pageId,
  inScopePageIds,
}: ShareReaderProps) {
  const navigate = useNavigate()

  // Update document title on mount + per page change. No live updates needed
  // in share-mode — pages.body and title are loaded once per route render.
  useEffect(() => {
    const previous = document.title
    const t = pageTitle && pageTitle.length > 0 ? pageTitle : 'Shared page'
    document.title = `${t} — tela`
    return () => {
      document.title = previous
    }
  }, [pageTitle])

  // Wikilink click interception. The decoration plugin paints two classes in
  // share-mode:
  //   - .tela-wikilink (in-scope) → navigate to /share/{token}/p/{id}
  //   - .tela-wikilink--share-out-of-scope (out-of-scope) → preventDefault
  // The link mark still emits an <a href="tela://page/N">; without this
  // handler the browser would attempt to navigate to the dead scheme.
  const wrapperRef = useRef<HTMLDivElement | null>(null)
  useEffect(() => {
    const el = wrapperRef.current
    if (!el) return
    function onClick(e: MouseEvent) {
      const target = (e.target as HTMLElement | null)?.closest(
        '.tela-wikilink, .tela-wikilink--share-out-of-scope',
      )
      if (!target) return
      // Out-of-scope: kill the navigation; the link is visually plain text but
      // the underlying <a> would otherwise attempt to follow `tela://page/N`.
      if (target.classList.contains('tela-wikilink--share-out-of-scope')) {
        e.preventDefault()
        return
      }
      // In-scope: resolve the page id from the underlying link mark and
      // navigate within share-mode. The descendant route serves the root id
      // too (backend includes the share root in the subtree scope), so we
      // don't need a special-case for it here.
      const anchor = target.closest('a')
      const href = anchor?.getAttribute('href') ?? ''
      const id = parseWikilinkPageId(href)
      if (id == null) return
      e.preventDefault()
      void navigate({
        to: '/share/$token/p/$pageId',
        params: { token, pageId: id },
      })
    }
    el.addEventListener('click', onClick)
    return () => el.removeEventListener('click', onClick)
  }, [navigate, token])

  // Stable Set reference — passing a fresh Set each render would force the
  // decoration plugin to rebuild on every parent re-render.
  const aliveIds = useMemo(() => inScopePageIds, [inScopePageIds])

  return (
    <div className="flex-1 flex flex-col gap-[var(--space-4)] p-[var(--space-7)] max-w-[48rem] w-full self-center min-h-0">
      <h1
        className={cn(
          'm-0 px-[var(--space-2)] py-[var(--space-2)]',
          'text-[length:var(--text-3xl)] leading-[var(--leading-tight)] font-medium',
          'text-[var(--text-primary)] font-[family-name:var(--font-sans)]',
        )}
      >
        {pageTitle || 'Untitled'}
      </h1>
      <div ref={wrapperRef}>
        <Suspense fallback={<EditorFallback />}>
          <MilkdownEditor
            key={`share-${pageId}`}
            defaultValue={pageBody}
            onChange={noop}
            ariaLabel="Shared page body"
            className={EDITOR_MIN_H}
            aliveWikilinkIds={aliveIds}
            collabPageId={null}
            readOnly
            wikilinkMode="share"
          />
        </Suspense>
      </div>
    </div>
  )
}

function noop() {
  // Share-mode is read-only — onChange is required by MilkdownEditor's
  // typing but should never fire because the editable predicate is gated by
  // readOnly. Kept as a noop in case Milkdown's listener fires once on mount
  // before the predicate is read (it would carry the same markdown back so
  // there's nothing meaningful to do with it).
}
