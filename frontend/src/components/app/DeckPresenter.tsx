import { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { ChevronLeft, ChevronRight, Download, Maximize, Pencil, PanelRight, X } from 'lucide-react'
import { api } from '../../lib/api'
import { Button } from '../ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'

interface DeckSlide {
  no: number
  title: string
  layout: string
  note: string
}

interface DeckManifest {
  id: string
  count: number
  variant?: string
  slides: string[]
  // Logical slides + a frame→slide map (frames can exceed slides with --with-clicks).
  outline?: DeckSlide[]
  slideForFrame?: number[]
}

const fmtClock = (s: number) =>
  `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`

// Full-screen Present for a deck page. The deck's Slidev markdown is rendered to
// per-frame PNGs by the backend (/api/pages/{id}/deck → the deck sidecar); this
// steps through them (one frame per click-step, so build animations show). A
// presenter panel (key: p) adds speaker notes + a next-frame preview + a timer —
// surfacing what Slidev already gives us, no Slidev runtime in the browser.
export function DeckPresenter({
  spaceId,
  pageId,
}: {
  spaceId: number
  pageId: number
}) {
  const navigate = useNavigate()
  const rootRef = useRef<HTMLDivElement>(null)
  const [i, setI] = useState(0)
  const [presenter, setPresenter] = useState(false)
  const [elapsed, setElapsed] = useState(0)

  // Render is content-hashed + cached server-side, so it's effectively immutable
  // for a given body — no need to refetch on focus.
  const { data = null, error } = useQuery({
    queryKey: ['deck-render', pageId],
    queryFn: () => api<DeckManifest>(`/api/pages/${pageId}/deck`),
    staleTime: Infinity,
    retry: false,
  })
  const err = error ? (error as Error).message || 'Failed to render deck' : null

  // Presenter timer — counts from when the panel opens (derived in the interval
  // callback, so no synchronous setState in the effect body).
  useEffect(() => {
    if (!presenter) return
    const start = Date.now()
    const t = setInterval(() => setElapsed(Math.floor((Date.now() - start) / 1000)), 250)
    return () => clearInterval(t)
  }, [presenter])

  const n = data?.count ?? 0
  const go = useCallback((d: number) => setI((p) => Math.max(0, Math.min(n - 1, p + d))), [n])
  const close = useCallback(() => {
    void navigate({
      to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
      params: { spaceId, pageId, slug: undefined },
      search: (p) => ({ ...p, view: undefined }),
    })
  }, [navigate, spaceId, pageId])
  const edit = useCallback(() => {
    void navigate({
      to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
      params: { spaceId, pageId, slug: undefined },
      search: (p) => ({ ...p, view: undefined, edit: true }),
    })
  }, [navigate, spaceId, pageId])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'ArrowRight' || e.key === ' ' || e.key === 'PageDown') {
        e.preventDefault()
        go(1)
      } else if (e.key === 'ArrowLeft' || e.key === 'PageUp') {
        e.preventDefault()
        go(-1)
      } else if (e.key === 'Escape') {
        close()
      } else if (e.key === 'f' || e.key === 'F') {
        rootRef.current?.requestFullscreen?.().catch(() => {})
      } else if (e.key === 'p' || e.key === 'P') {
        setPresenter((v) => !v)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [go, close])

  // Logical slide + speaker note for the current frame.
  const logical = data?.slideForFrame?.[i] ?? i
  const note = data?.outline?.[logical]?.note ?? ''
  const totalSlides = data?.outline?.length ?? n

  return (
    <div ref={rootRef} className="flex h-full w-full flex-col bg-[var(--surface-3)]">
      <div className="grid min-h-0 flex-1 grid-cols-[1fr] gap-[var(--space-4)] p-[var(--space-5)] lg:grid-cols-[minmax(0,1fr)_auto]">
        <div className="grid min-h-0 place-items-center">
          {err ? (
            <div className="text-center text-[var(--text-muted)]">
              <p>Couldn't render this deck.</p>
              <p className="mt-[var(--space-2)] text-[var(--text-sm)]">{err}</p>
            </div>
          ) : !data ? (
            <p className="text-[var(--text-muted)]">Rendering deck…</p>
          ) : (
            <img
              src={data.slides[i]}
              alt={`Slide ${i + 1} of ${n}`}
              className="max-h-full max-w-full rounded-[var(--radius-md)] object-contain shadow-2xl"
            />
          )}
        </div>

        {/* Presenter panel — next frame + speaker note + timer. Surfaces Slidev's
            own speaker notes; no extra mechanics. */}
        {presenter && data ? (
          <aside className="flex w-full min-w-0 flex-col gap-[var(--space-3)] lg:w-[22rem]">
            <div className="rounded-[var(--radius-md)] bg-[var(--surface-2)] p-[var(--space-3)]">
              <div className="mb-[var(--space-2)] flex items-center justify-between text-[var(--text-xs)] uppercase tracking-wide text-[var(--text-muted)]">
                <span>Next</span>
                <span className="tabular-nums">{fmtClock(elapsed)}</span>
              </div>
              {i + 1 < n ? (
                <img
                  src={data.slides[i + 1]}
                  alt={`Next slide`}
                  className="w-full rounded-[var(--radius-sm)] object-contain opacity-90"
                />
              ) : (
                <div className="grid h-24 place-items-center text-[var(--text-sm)] text-[var(--text-muted)]">
                  End of deck
                </div>
              )}
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto rounded-[var(--radius-md)] bg-[var(--surface-2)] p-[var(--space-3)]">
              <div className="mb-[var(--space-2)] text-[var(--text-xs)] uppercase tracking-wide text-[var(--text-muted)]">
                Notes · slide {logical + 1} / {totalSlides}
              </div>
              {note ? (
                <p className="whitespace-pre-wrap text-[var(--text-sm)] leading-[var(--leading-relaxed)] text-[var(--text-primary)]">
                  {note}
                </p>
              ) : (
                <p className="text-[var(--text-sm)] italic text-[var(--text-muted)]">No notes for this slide.</p>
              )}
            </div>
          </aside>
        ) : null}
      </div>

      <div className="flex items-center justify-center gap-[var(--space-2)] border-t border-[var(--border-subtle)] bg-[var(--surface-2)] p-[var(--space-3)]">
        <Button variant="ghost" size="sm" onClick={() => go(-1)} disabled={i === 0} aria-label="Previous slide">
          <ChevronLeft width={18} height={18} />
        </Button>
        <span className="min-w-[3.5rem] text-center text-[var(--text-sm)] tabular-nums text-[var(--text-muted)]">
          {n ? i + 1 : 0} / {n}
        </span>
        <Button variant="ghost" size="sm" onClick={() => go(1)} disabled={i >= n - 1} aria-label="Next slide">
          <ChevronRight width={18} height={18} />
        </Button>
        <span className="mx-[var(--space-2)] h-4 w-px bg-[var(--border-subtle)]" />
        <Button
          variant={presenter ? 'primary' : 'ghost'}
          size="sm"
          onClick={() => setPresenter((v) => !v)}
          aria-label="Presenter notes"
          aria-pressed={presenter}
        >
          <PanelRight width={16} height={16} />
        </Button>
        <Button variant="ghost" size="sm" onClick={() => rootRef.current?.requestFullscreen?.().catch(() => {})} aria-label="Fullscreen">
          <Maximize width={16} height={16} />
        </Button>
        <Button variant="ghost" size="sm" onClick={edit} aria-label="Edit deck">
          <Pencil width={16} height={16} />
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm" aria-label="Download">
              <Download width={16} height={16} />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem asChild>
              <a href={`/api/pages/${pageId}/deck.pdf`} target="_blank" rel="noreferrer">
                Download PDF
              </a>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <a href={`/api/pages/${pageId}/deck.pptx`} target="_blank" rel="noreferrer">
                Download PPTX
              </a>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <Button variant="ghost" size="sm" onClick={close} aria-label="Close presentation">
          <X width={16} height={16} />
        </Button>
      </div>
    </div>
  )
}
