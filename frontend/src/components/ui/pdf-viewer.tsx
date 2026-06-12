import { lazy, Suspense } from 'react'
import { Download, ExternalLink, FileText, Loader2 } from 'lucide-react'
import { Dialog, DialogContent, DialogTitle } from './dialog'

// pdf-viewer — the light half: an `isPdf` guard and a modal that lazy-loads the
// heavy react-pdf engine only when opened. Consumers (file cards, attachment
// chips) own the open state and just render <PdfPreviewDialog> alongside their
// own trigger; nothing pulls pdf.js into the main bundle until a preview opens.

const PdfDocument = lazy(() => import('./pdf-document'))

// isPdf — true when a name/url ends in .pdf or the mime is application/pdf.
// eslint-disable-next-line react-refresh/only-export-components
export function isPdf(nameOrUrl?: string | null, mime?: string | null): boolean {
  if (mime && mime.toLowerCase() === 'application/pdf') return true
  if (!nameOrUrl) return false
  return /\.pdf(?:[?#]|$)/i.test(nameOrUrl)
}

export interface PdfPreviewDialogProps {
  url: string
  name?: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function PdfPreviewDialog({
  url,
  name,
  open,
  onOpenChange,
}: PdfPreviewDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="tela-pdf-dialog">
        <div className="flex items-center justify-between gap-[var(--space-3)] pr-[calc(var(--space-7)+var(--space-3))]">
          <DialogTitle className="tela-pdf-dialog-title">
            <FileText width={16} height={16} aria-hidden />
            <span className="truncate">{name || 'PDF'}</span>
          </DialogTitle>
          <div className="tela-pdf-dialog-actions">
            <a
              href={url}
              target="_blank"
              rel="noopener noreferrer"
              aria-label="Open in new tab"
            >
              <ExternalLink width={16} height={16} aria-hidden />
            </a>
            <a href={url} download={name || undefined} aria-label="Download">
              <Download width={16} height={16} aria-hidden />
            </a>
          </div>
        </div>
        <Suspense
          fallback={
            <div className="tela-pdf-status">
              <Loader2
                className="tela-pdf-spin"
                width={18}
                height={18}
                aria-hidden
              />
              <span>Loading viewer…</span>
            </div>
          }
        >
          {open ? <PdfDocument url={url} /> : null}
        </Suspense>
      </DialogContent>
    </Dialog>
  )
}
