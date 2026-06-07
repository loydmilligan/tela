import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { usePage } from '../../lib/queries/pages'

// Hover preview for internal page links — both `[[Name]]` bracket wikilinks and
// picker-inserted `[Title](tela://page/{id})` links (anything resolving to a
// `tela://page/{id}` anchor). Hovering one for a beat pops a small card with the
// target page's title + an excerpt of its body, so you can peek a linked page
// without navigating. Authed surfaces only — it fetches the page via the authed
// API (usePage), so it is NOT mounted in share/print readers.

const HREF_PREFIX = 'tela://page/'
const CARD_WIDTH = 320
const SHOW_DELAY_MS = 350
const HIDE_DELAY_MS = 120

function parseTelaPageId(href: string | null): number | null {
  if (!href || !href.startsWith(HREF_PREFIX)) return null
  const tail = href.slice(HREF_PREFIX.length)
  return /^\d+$/.test(tail) ? Number(tail) : null
}

// Strip the loudest markdown so the excerpt reads as prose — not a full parse,
// just enough that fences/links/headings don't leak syntax into the preview.
function previewExcerpt(body: string, maxChars = 240): string {
  const text = body
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`[^`]*`/g, ' ')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[\[([^[\]|#]+)(?:[|#][^[\]]*)?\]\]/g, '$1') // [[Name|alias]] → Name
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/^\s{0,3}#{1,6}\s+/gm, '')
    .replace(/[*_~>`]/g, '')
    .replace(/\s+/g, ' ')
    .trim()
  if (text.length <= maxChars) return text
  return text.slice(0, maxChars).replace(/\s+\S*$/, '') + '…'
}

// ---- presentational card (Storybook-covered) -------------------------------

export interface WikilinkPreviewCardProps {
  // Viewport-relative anchor rect the card positions against (position: fixed).
  rect: { left: number; top: number; bottom: number }
  title: string
  excerpt: string
  loading: boolean
}

export function WikilinkPreviewCard({
  rect,
  title,
  excerpt,
  loading,
}: WikilinkPreviewCardProps) {
  // Default placement is below-left of the link; flip above when the link sits
  // in the lower part of the viewport so the card stays on-screen. Anchoring via
  // top OR bottom means we never need to know the card's height up front.
  const placeAbove = rect.bottom > window.innerHeight * 0.6
  const left = Math.max(
    12,
    Math.min(rect.left, window.innerWidth - CARD_WIDTH - 12),
  )

  return (
    <div
      role="tooltip"
      className="tela-wikilink-preview"
      style={{
        left,
        ...(placeAbove
          ? { bottom: window.innerHeight - rect.top + 8 }
          : { top: rect.bottom + 8 }),
      }}
    >
      <p className="tela-wikilink-preview-title">{title || 'Untitled'}</p>
      {loading ? (
        <div className="tela-wikilink-preview-skel" aria-hidden>
          <span />
          <span />
        </div>
      ) : excerpt ? (
        <p className="tela-wikilink-preview-excerpt">{excerpt}</p>
      ) : (
        <p className="tela-wikilink-preview-empty">Empty page</p>
      )}
    </div>
  )
}

// ---- controller: delegated hover + fetch -----------------------------------

interface HoverTarget {
  id: number
  rect: { left: number; top: number; bottom: number }
  label: string
}

export interface WikilinkHoverPreviewProps {
  // Surface whose `tela://page/{id}` anchors get previews (editor body / reader
  // article). Hover detection is delegated to this container.
  containerRef: React.RefObject<HTMLElement | null>
}

export function WikilinkHoverPreview({ containerRef }: WikilinkHoverPreviewProps) {
  const [target, setTarget] = useState<HoverTarget | null>(null)
  const showTimer = useRef<number | undefined>(undefined)
  const hideTimer = useRef<number | undefined>(undefined)

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const clearTimers = () => {
      window.clearTimeout(showTimer.current)
      window.clearTimeout(hideTimer.current)
    }
    const onOver = (e: MouseEvent) => {
      const anchor = (e.target as HTMLElement | null)?.closest('a')
      if (!anchor) return
      const id = parseTelaPageId(anchor.getAttribute('href'))
      if (id == null) return
      clearTimers()
      const r = anchor.getBoundingClientRect()
      const label = anchor.textContent ?? ''
      showTimer.current = window.setTimeout(() => {
        setTarget({ id, rect: { left: r.left, top: r.top, bottom: r.bottom }, label })
      }, SHOW_DELAY_MS)
    }
    const onOut = (e: MouseEvent) => {
      const anchor = (e.target as HTMLElement | null)?.closest('a')
      if (!anchor) return
      window.clearTimeout(showTimer.current)
      hideTimer.current = window.setTimeout(() => setTarget(null), HIDE_DELAY_MS)
    }
    el.addEventListener('mouseover', onOver)
    el.addEventListener('mouseout', onOut)
    return () => {
      el.removeEventListener('mouseover', onOver)
      el.removeEventListener('mouseout', onOut)
      clearTimers()
    }
  }, [containerRef])

  // A scroll or resize invalidates the captured rect — just dismiss.
  useEffect(() => {
    if (!target) return
    const dismiss = () => setTarget(null)
    window.addEventListener('scroll', dismiss, true)
    window.addEventListener('resize', dismiss)
    return () => {
      window.removeEventListener('scroll', dismiss, true)
      window.removeEventListener('resize', dismiss)
    }
  }, [target])

  if (!target) return null
  return <PreviewForId target={target} />
}

function PreviewForId({ target }: { target: HoverTarget }) {
  const { data, isLoading } = usePage(target.id)
  return createPortal(
    <WikilinkPreviewCard
      rect={target.rect}
      title={data?.title ?? target.label}
      excerpt={data ? previewExcerpt(data.body) : ''}
      loading={isLoading && !data}
    />,
    document.body,
  )
}
