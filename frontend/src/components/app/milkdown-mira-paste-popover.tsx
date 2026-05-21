import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { ApiError } from '../../lib/api'
import { useImportMira } from '../../lib/queries/imports'
import type { Page } from '../../lib/types'
import { Button } from '../ui/button'
import { cn } from '../../lib/utils'

// M18.B.2 — inline popover offered when the user pastes a mira URL into the
// editor. Three actions: import-as-child (default), keep-as-link, cancel.
//
// Positioning follows the slash-menu's direct-DOM pattern (see
// `milkdown-slash.tsx` comment block on the SlashProvider debounce wedge):
// `position: fixed` with JS-set left/top derived from the caret coords the
// plugin captured at paste time. One rAF re-measure after first paint flips
// the popover upward if it would overflow the viewport bottom and clamps it
// inside the viewport on both axes.
//
// Dismiss surfaces:
// 1. Action buttons — close after the action runs.
// 2. Outside-click — pointer-down anywhere outside the popover root.
// 3. Escape — capture-phase keydown so we beat ProseMirror's handler.
// 4. Error fallback — after a 3s auto-dismiss timer.
//
// Q8: no new primitive. Uses the existing `Button` primitive + tokens for
// chrome. Inline error rendered as a `<p role="alert">` — same pattern as
// the rest of the app (Settings forms etc.).

export interface MiraPastePopoverProps {
  // Pasted URL — rendered as the small preview line above the action row.
  url: string
  // Screen-space caret coords from the plugin. Used to anchor the popover.
  anchor: { left: number; top: number; bottom: number }
  // Target space + parent page for the import request. Both required; the
  // host (`MilkdownEditor`) only renders the popover when both are known.
  spaceId: number
  parentPageId: number
  // Fires with the created page on a successful import. The host bridges to
  // the plugin's `insertWikilink(id, title)` callback so the canonical
  // `[Title](tela://page/{id})` mark replaces what would have been pasted.
  onImportComplete: (page: Page) => void
  // Fires when the user picks "Keep as link" OR when an import error needs
  // to fall back to a plain link. The host bridges to the plugin's
  // `insertPlainLink()` callback.
  onKeepAsLink: () => void
  // Cancel / outside-click / Escape / post-error auto-dismiss all close the
  // popover. The host clears its `miraPastePopover` state, which unmounts
  // this component.
  onCancel: () => void
}

// Auto-dismiss delay after a fallback-to-plain-link error message.
const ERROR_DISMISS_MS = 3000

export function MiraPastePopover({
  url,
  anchor,
  spaceId,
  parentPageId,
  onImportComplete,
  onKeepAsLink,
  onCancel,
}: MiraPastePopoverProps) {
  const rootRef = useRef<HTMLDivElement>(null)
  const importMira = useImportMira()
  const [error, setError] = useState<string | null>(null)
  // Cancellation guard. Outside-click / Escape can dismiss the popover while
  // a `mutateAsync` is still in flight; when it resolves we must skip the
  // insertion (the user effectively cancelled). The created mira page row
  // still exists in the DB and is reachable from the sidebar — acceptable
  // trade-off vs. either disabling dismissal during import or rolling back
  // server-side state.
  const cancelledRef = useRef(false)
  useEffect(() => {
    return () => {
      cancelledRef.current = true
    }
  }, [])

  // Initial anchor placement, set before paint via useLayoutEffect so the
  // popover never flashes at (0, 0). One rAF later we re-measure and flip /
  // clamp if needed, mirroring the slash-menu pattern.
  useLayoutEffect(() => {
    const el = rootRef.current
    if (!el) return
    el.style.left = `${anchor.left}px`
    el.style.top = `${anchor.bottom + 4}px`
    const rafId = requestAnimationFrame(() => {
      const r = el.getBoundingClientRect()
      const vh = window.innerHeight
      const vw = window.innerWidth
      let top = anchor.bottom + 4
      if (top + r.height > vh && anchor.top > vh - anchor.bottom) {
        top = anchor.top - r.height - 4
      }
      top = Math.max(4, Math.min(top, vh - r.height - 4))
      let left = anchor.left
      if (left + r.width > vw) {
        left = vw - r.width - 4
      }
      left = Math.max(4, left)
      el.style.top = `${top}px`
      el.style.left = `${left}px`
    })
    return () => cancelAnimationFrame(rafId)
  }, [anchor])

  // Escape — capture phase so we beat ProseMirror's keydown handler.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        onCancel()
      }
    }
    document.addEventListener('keydown', onKey, true)
    return () => document.removeEventListener('keydown', onKey, true)
  }, [onCancel])

  // Outside-click. Pointer-down rather than click so the dismiss fires before
  // the editor's own click handlers re-focus / move the caret.
  useEffect(() => {
    function onDown(e: PointerEvent) {
      const root = rootRef.current
      if (!root) return
      const target = e.target
      if (target instanceof Node && root.contains(target)) return
      onCancel()
    }
    document.addEventListener('pointerdown', onDown, true)
    return () => document.removeEventListener('pointerdown', onDown, true)
  }, [onCancel])

  // After an import error we keep the popover visible with the message for a
  // brief window so the user notices what happened, then dismiss.
  useEffect(() => {
    if (!error) return
    const t = window.setTimeout(() => {
      onCancel()
    }, ERROR_DISMISS_MS)
    return () => window.clearTimeout(t)
  }, [error, onCancel])

  async function handleImport() {
    if (importMira.isPending) return
    try {
      const res = await importMira.mutateAsync({
        spaceId,
        parentId: parentPageId,
        sourceUrl: url,
      })
      if (cancelledRef.current) return
      onImportComplete(res.page)
    } catch (err) {
      if (cancelledRef.current) return
      // Fallback: still give the user the URL in the doc — losing the paste
      // would be a worse outcome than not getting the import. Then show the
      // error inline for ERROR_DISMISS_MS before auto-dismissing.
      onKeepAsLink()
      const message =
        err instanceof ApiError
          ? `Import failed: ${err.message}`
          : 'Import failed. URL kept as link.'
      setError(message)
    }
  }

  // After an error the fallback plain link is already in the doc; further
  // clicks would either re-insert (broken) or fight the auto-dismiss. Lock
  // the action row until the popover unmounts.
  const actionsLocked = importMira.isPending || error != null

  return (
    <div
      ref={rootRef}
      role="dialog"
      aria-label="Import pasted mira link"
      className="tela-mira-paste-popover"
      data-show="true"
    >
      <p className="tela-mira-paste-popover-url" title={url}>
        {url}
      </p>
      <div className="tela-mira-paste-popover-actions">
        <Button
          type="button"
          variant="primary"
          size="sm"
          onClick={() => void handleImport()}
          disabled={actionsLocked}
        >
          {importMira.isPending ? 'Importing…' : 'Import as child'}
        </Button>
        <Button
          type="button"
          variant="secondary"
          size="sm"
          onClick={() => {
            if (actionsLocked) return
            onKeepAsLink()
            onCancel()
          }}
          disabled={actionsLocked}
        >
          Keep as link
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => {
            if (actionsLocked) return
            onCancel()
          }}
          disabled={actionsLocked}
        >
          Cancel
        </Button>
      </div>
      {error ? (
        <p
          role="alert"
          className={cn(
            'tela-mira-paste-popover-error',
            'm-0 text-[length:var(--text-xs)] text-[var(--danger)]',
          )}
        >
          {error}
        </p>
      ) : null}
    </div>
  )
}
