import { FileDown, Loader2 } from 'lucide-react'
import { Button } from '../ui/button'
import { cn } from '../../lib/utils'
import { useFileDownload } from './use-file-download'

interface DownloadPdfButtonProps {
  /** Endpoint that streams the PDF (authed /api/pages/{id}/pdf or a /share/…/.pdf URL). */
  url: string
  label?: string
  fallbackName?: string
  className?: string
  /** Append the *current* theme (?theme=…) at click time so the PDF matches the
   * user's selected theme — read live, not baked in at render. */
  themed?: boolean
}

// Fetches a server-rendered PDF and triggers a download, showing a pending
// state while gotenberg renders (a few seconds). Uses fetch→Blob rather than a
// plain <a download> so we can show progress and surface a failure. credentials
// are included for the session-authed page endpoint; harmless on public shares.
export function DownloadPdfButton({
  url,
  label = 'Download PDF',
  fallbackName = 'page.pdf',
  className,
  themed = false,
}: DownloadPdfButtonProps) {
  const { download, busy, failed } = useFileDownload(url, { themed, fallbackName })

  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      aria-label={label}
      onClick={() => void download()}
      disabled={busy}
      className={cn('h-[var(--space-8)] px-[var(--space-3)]', className)}
    >
      {busy ? (
        <Loader2 width={16} height={16} className="animate-spin" />
      ) : (
        <FileDown width={16} height={16} />
      )}
      <span>{busy ? 'Generating…' : failed ? 'Retry PDF' : label}</span>
    </Button>
  )
}
