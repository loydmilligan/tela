import { Suspense, lazy } from 'react'
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '../ui/sheet'

const LocalGraph = lazy(() => import('./LocalGraph'))

// Placement B — the right-rail graph panel, a non-modal Sheet beside the editor
// (same mechanics as the comments panel). The graph engine is lazy-imported and
// the Sheet only renders its body when open, so opening the panel is the first
// time PageGraph + d3-force load — nothing on the editor's critical path.

export function LocalGraphPanel({
  pageId,
  open,
  onOpenChange,
}: {
  pageId: number
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange} modal={false}>
      <SheetContent
        side="right"
        className="flex flex-col"
        withOverlay={false}
        onOpenAutoFocus={(e) => e.preventDefault()}
        onInteractOutside={(e) => e.preventDefault()}
      >
        <SheetHeader>
          <SheetTitle>Graph</SheetTitle>
          <SheetDescription>
            This page and the pages it connects to.
          </SheetDescription>
        </SheetHeader>
        <SheetBody className="flex flex-1 flex-col">
          <div className="min-h-[calc(var(--space-8)*8)] flex-1 overflow-hidden rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-1)]">
            {open ? (
              <Suspense fallback={<div className="h-full w-full bg-[var(--surface-2)]" aria-hidden />}>
                <LocalGraph pageId={pageId} depth={2} />
              </Suspense>
            ) : null}
          </div>
        </SheetBody>
      </SheetContent>
    </Sheet>
  )
}
