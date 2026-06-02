import { useState } from 'react'
import { FileDown, Loader2 } from 'lucide-react'
import { Button } from '../ui/button'
import { cn } from '../../lib/utils'
import { getTheme } from '../../lib/theme'

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
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)

  async function download() {
    if (busy) return
    setBusy(true)
    setFailed(false)
    try {
      const target = themed
        ? url + (url.includes('?') ? '&' : '?') + 'theme=' + getTheme()
        : url
      const res = await fetch(target, { credentials: 'include' })
      if (!res.ok) throw new Error(`pdf ${res.status}`)
      const blob = await res.blob()
      const cd = res.headers.get('Content-Disposition') ?? ''
      const match = /filename="?([^"]+)"?/.exec(cd)
      const name = match?.[1] ?? fallbackName
      const objUrl = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = objUrl
      a.download = name
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(objUrl)
    } catch {
      setFailed(true)
    } finally {
      setBusy(false)
    }
  }

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
