import { useEffect, useRef, useState } from 'react'

// A deck's first-slide cover image. The cover endpoint renders on demand
// server-side (and the sidecar caps concurrency), so a cold or queued render can
// momentarily 502 / time out even though the frame is — or is about to be —
// cached by the background warmer. A plain <img onError> that hides forever then
// strands a cover that's actually ready ("it's generated but still doesn't
// show"). So: show a skeleton while loading, and on error retry with backoff +
// cache-bust a few times before giving up.
const MAX_RETRIES = 4
const backoffMs = (attempt: number) => Math.min(500 * 2 ** attempt, 4000) // 500ms,1s,2s,4s

export function DeckCoverImage({
  src,
  alt = '',
  loading = 'lazy',
  className,
  onGiveUp,
}: {
  src: string
  alt?: string
  loading?: 'lazy' | 'eager'
  className?: string
  // Called when every retry is exhausted; the component then renders nothing so
  // the parent can show its own fallback (gradient, placeholder, …).
  onGiveUp?: () => void
}) {
  const [status, setStatus] = useState<'loading' | 'ok' | 'failed'>('loading')
  const [attempt, setAttempt] = useState(0)
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)

  // Reset when the underlying src changes (variant switch, page change) — the
  // React "adjust state during render" pattern, no effect needed.
  const [prevSrc, setPrevSrc] = useState(src)
  if (src !== prevSrc) {
    setPrevSrc(src)
    setStatus('loading')
    setAttempt(0)
  }
  useEffect(() => () => clearTimeout(timer.current), [])

  // Attempt 0 uses the bare URL so a warm cover hits the browser cache; retries
  // append a unique param so a failed GET isn't served from the negative cache.
  const url = attempt === 0 ? src : `${src}${src.includes('?') ? '&' : '?'}r=${attempt}`

  const onError = () => {
    if (attempt >= MAX_RETRIES) {
      setStatus('failed')
      onGiveUp?.()
      return
    }
    const next = attempt + 1
    timer.current = setTimeout(() => {
      setStatus('loading')
      setAttempt(next)
    }, backoffMs(attempt))
  }

  if (status === 'failed') return null
  return (
    <>
      {status === 'loading' ? (
        <div className="absolute inset-0 animate-pulse bg-[var(--surface-3)]" aria-hidden />
      ) : null}
      <img
        key={url}
        src={url}
        alt={alt}
        loading={loading}
        onLoad={() => setStatus('ok')}
        onError={onError}
        className={className}
        style={status === 'ok' ? undefined : { visibility: 'hidden' }}
      />
    </>
  )
}
