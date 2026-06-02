import {
  Suspense,
  lazy,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { Type } from 'lucide-react'
import type { EditorView } from '@milkdown/kit/prose/view'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import {
  getTheme,
  setTheme,
  subscribeToTheme,
  THEMES,
  type ThemeName,
} from '../../lib/theme'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'
import { ToggleGroup, ToggleGroupItem } from '../ui/toggle'

// Lazy — the Milkdown grammar/Yjs blob is the app's largest dep and ships as
// its own chunk; neither reader root (authed /read, public /share) should drag
// it into the main entry.
const MilkdownEditor = lazy(() =>
  import('./milkdown-editor').then((m) => ({ default: m.MilkdownEditor })),
)

// Inlined (not imported from milkdown-wikilink-decoration.ts) so Rollup doesn't
// pull the wikilink-decoration module in as a shared dep of the reader chunk —
// that split was producing a separate ~320 KB chunk. 5-line dupe is cheaper.
function parseWikilinkPageId(href: string): number | null {
  const prefix = 'tela://page/'
  if (!href.startsWith(prefix)) return null
  const tail = href.slice(prefix.length)
  if (!/^\d+$/.test(tail)) return null
  return Number(tail)
}

type ReaderSize = 's' | 'm' | 'l'
type ReaderFont = 'sans' | 'serif'
const SIZE_KEY = 'tela:reader:size'
const FONT_KEY = 'tela:reader:font'
const WORDS_PER_MIN = 220

function readPref<T extends string>(key: string, fallback: T, valid: readonly T[]): T {
  try {
    const v = localStorage.getItem(key)
    return v && (valid as readonly string[]).includes(v) ? (v as T) : fallback
  } catch {
    return fallback
  }
}

function writePref(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // private-mode / quota — preference just won't persist this session.
  }
}

// Rough word count for the reading-time estimate. Strips the loudest markdown
// noise so fenced code / link URLs don't inflate the count — good enough for an
// at-a-glance "N min read", not a parser.
function readingMinutes(body: string): number {
  const text = body
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/`[^`]*`/g, ' ')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/[#>*_~`-]/g, ' ')
  const words = text.split(/\s+/).filter(Boolean).length
  return Math.max(1, Math.round(words / WORDS_PER_MIN))
}

interface TocEntry {
  id: string
  text: string
  level: number
}

export interface ReaderShellProps {
  /** Page being read — drives the editor key + reading-time/meta. */
  pageId: number
  title: string
  body: string
  updatedAt: string
  /** Decoration mode for the read-only editor. */
  wikilinkMode: 'edit' | 'share'
  /**
   * Ids the wikilink decoration treats as "alive"/in-scope. In read mode this
   * is every page; in share mode it's the in-scope subtree.
   */
  aliveWikilinkIds: Set<number> | null
  /**
   * Navigation policy for a clicked wikilink. The shell preventDefaults every
   * `tela://page/N` anchor (the scheme is dead to the browser); the caller
   * decides whether/where to navigate (no-op for out-of-scope or broken).
   */
  onNavigateWikilink: (targetPageId: number) => void
  /** Far-left of the top bar — close button (read) or wordmark (share). */
  topbarLeading?: ReactNode
  /** Right of the top bar, before the Display/Print controls — e.g. Sign in. */
  topbarTrailing?: ReactNode
  /** Optional persistent left rail (share subtree nav). */
  sidebar?: ReactNode
  /** Bound to Escape when provided (read mode → back to editor). */
  onEscape?: () => void
  /** Optional source line in the cover meta (e.g. the canonical/share URL).
   * Shown in print/PDF and share contexts so an exported doc reads as published. */
  sourceLabel?: string
}

// Set on the window once the reader has painted (fonts ready + a short settle so
// async mermaid/katex/diagrams land). gotenberg's PDF export waits on this flag
// before capturing — see backend renderPDF (waitForExpression).
declare global {
  interface Window {
    __telaPdfReady?: boolean
  }
}

// The shared reading surface: chrome-free, full-bleed, read-only page render
// with size/typeface/theme controls, a scroll-spy TOC, reading-progress, and a
// print stylesheet that doubles as Save-as-PDF. Presentational only — auth and
// data-fetching live in the route callers (PageReader / ShareReaderView).
export function ReaderShell({
  pageId,
  title,
  body,
  updatedAt,
  wikilinkMode,
  aliveWikilinkIds,
  onNavigateWikilink,
  topbarLeading,
  topbarTrailing,
  sidebar,
  onEscape,
  sourceLabel,
}: ReaderShellProps) {
  // Preferences — text size + typeface, persisted; theme is global.
  const [size, setSize] = useState<ReaderSize>(() =>
    readPref<ReaderSize>(SIZE_KEY, 'm', ['s', 'm', 'l']),
  )
  const [font, setFont] = useState<ReaderFont>(() =>
    readPref<ReaderFont>(FONT_KEY, 'sans', ['sans', 'serif']),
  )
  const [theme, setThemeState] = useState<ThemeName>(() => getTheme())
  useEffect(() => subscribeToTheme(setThemeState), [])

  const minutes = useMemo(() => readingMinutes(body), [body])

  // Document title while in the reader.
  useEffect(() => {
    const prev = document.title
    document.title = `${title || 'Untitled'} — tela`
    return () => {
      document.title = prev
    }
  }, [title])

  // Esc exits (read mode → editor). No-op when the caller has nowhere to go.
  useEffect(() => {
    if (!onEscape) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        onEscape!()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onEscape])

  // --- TOC + scroll-spy + progress ---------------------------------------
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const progressRef = useRef<HTMLDivElement | null>(null)
  const topbarRef = useRef<HTMLElement | null>(null)
  const headingsRef = useRef<HTMLElement[]>([])
  const [toc, setToc] = useState<TocEntry[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  const activeRef = useRef<string | null>(null)

  // Build the TOC from the rendered heading DOM once the editor view is ready.
  // Markdown→DOM heading order is stable, so reading straight off view.dom keeps
  // the TOC guaranteed in-sync with what's on screen.
  const handleViewReady = useCallback((view: EditorView | null) => {
    if (!view) return
    requestAnimationFrame(() => {
      const els = Array.from(
        view.dom.querySelectorAll('h1, h2, h3'),
      ) as HTMLElement[]
      const entries: TocEntry[] = []
      els.forEach((el, i) => {
        const text = (el.textContent ?? '').trim()
        if (!text) return
        if (!el.id) el.id = `reader-h-${i}`
        el.classList.add('reader-heading')
        entries.push({ id: el.id, text, level: Number(el.tagName[1]) })
      })
      headingsRef.current = els.filter((el) => el.id && el.textContent?.trim())
      setToc(entries)
      // PDF export readiness signal (gotenberg waits on this). Wait for fonts +
      // a short settle so async mermaid/katex/diagram paints land in the capture.
      const ready = () =>
        window.setTimeout(() => {
          window.__telaPdfReady = true
        }, 400)
      if (document.fonts?.ready) void document.fonts.ready.finally(ready)
      else ready()
    })
  }, [])

  useEffect(() => {
    const sc = scrollRef.current
    if (!sc) return
    let raf = 0
    const update = () => {
      raf = 0
      const max = sc.scrollHeight - sc.clientHeight
      const p = max > 0 ? sc.scrollTop / max : 0
      if (progressRef.current) {
        progressRef.current.style.setProperty('--reader-progress', String(p))
      }
      if (topbarRef.current) {
        topbarRef.current.dataset.scrolled = sc.scrollTop > 4 ? 'true' : 'false'
      }
      // Active heading = last one whose top has crossed a band below the bar.
      const threshold = sc.getBoundingClientRect().top + 96
      let next: string | null = headingsRef.current[0]?.id ?? null
      for (const el of headingsRef.current) {
        if (el.getBoundingClientRect().top <= threshold) next = el.id
        else break
      }
      if (next !== activeRef.current) {
        activeRef.current = next
        setActiveId(next)
      }
    }
    const onScroll = () => {
      if (!raf) raf = requestAnimationFrame(update)
    }
    sc.addEventListener('scroll', onScroll, { passive: true })
    update()
    return () => {
      sc.removeEventListener('scroll', onScroll)
      if (raf) cancelAnimationFrame(raf)
    }
  }, [toc])

  const jumpTo = useCallback((id: string) => {
    const el = document.getElementById(id)
    if (!el) return
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    el.scrollIntoView({ behavior: reduce ? 'auto' : 'smooth', block: 'start' })
  }, [])

  // Wikilink navigation — keep clicks inside the reader. Capture phase so we run
  // before the editor's own broken-wikilink listener (which would otherwise open
  // the new-page dialog). We preventDefault every tela:// link and defer the
  // actual navigation decision to the caller's policy.
  const articleRef = useRef<HTMLDivElement | null>(null)
  useEffect(() => {
    const el = articleRef.current
    if (!el) return
    function onClick(e: MouseEvent) {
      const anchor = (e.target as HTMLElement | null)?.closest('a')
      if (!anchor) return
      const id = parseWikilinkPageId(anchor.getAttribute('href') ?? '')
      if (id == null) return
      e.preventDefault()
      e.stopPropagation()
      onNavigateWikilink(id)
    }
    el.addEventListener('click', onClick, true)
    return () => el.removeEventListener('click', onClick, true)
  }, [onNavigateWikilink])

  return (
    <div className="tela-reader" data-reading-size={size} data-reading-font={font}>
      <div ref={progressRef} className="reader-progress" aria-hidden />

      <header ref={topbarRef} className="reader-topbar" data-scrolled="false">
        <div className="reader-topbar-left">
          {topbarLeading}
          <span className="reader-topbar-title">{title || 'Untitled'}</span>
        </div>

        <div className="reader-topbar-right">
          {topbarTrailing}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                aria-label="Reading display options"
                className="h-[var(--space-8)] px-[var(--space-3)]"
              >
                <Type width={16} height={16} />
                <span>Display</span>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <div className="reader-prefs">
                <div className="reader-prefs-group">
                  <span className="reader-prefs-label">Text size</span>
                  <ToggleGroup
                    type="single"
                    value={size}
                    onValueChange={(v) => {
                      if (!v) return
                      setSize(v as ReaderSize)
                      writePref(SIZE_KEY, v)
                    }}
                    aria-label="Text size"
                  >
                    <ToggleGroupItem value="s">Small</ToggleGroupItem>
                    <ToggleGroupItem value="m">Medium</ToggleGroupItem>
                    <ToggleGroupItem value="l">Large</ToggleGroupItem>
                  </ToggleGroup>
                </div>
                <div className="reader-prefs-group">
                  <span className="reader-prefs-label">Typeface</span>
                  <ToggleGroup
                    type="single"
                    value={font}
                    onValueChange={(v) => {
                      if (!v) return
                      setFont(v as ReaderFont)
                      writePref(FONT_KEY, v)
                    }}
                    aria-label="Typeface"
                  >
                    <ToggleGroupItem value="sans">Sans</ToggleGroupItem>
                    <ToggleGroupItem value="serif">Serif</ToggleGroupItem>
                  </ToggleGroup>
                </div>
                <div className="reader-prefs-group">
                  <span className="reader-prefs-label">Theme</span>
                  <ToggleGroup
                    type="single"
                    value={theme}
                    onValueChange={(v) => {
                      if (!v) return
                      setTheme(v as ThemeName)
                      setThemeState(v as ThemeName)
                    }}
                    aria-label="Theme"
                  >
                    {THEMES.map((t) => (
                      <ToggleGroupItem key={t} value={t} className="capitalize">
                        {t}
                      </ToggleGroupItem>
                    ))}
                  </ToggleGroup>
                </div>
              </div>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </header>

      <div className="reader-body">
        {sidebar}
        <div ref={scrollRef} className="reader-scroll">
          <div className="reader-grid">
            {toc.length > 1 ? (
              <nav className="reader-toc" aria-label="On this page">
                <p className="reader-toc-label">On this page</p>
                <ul className="reader-toc-list">
                  {toc.map((entry) => (
                    <li key={entry.id}>
                      <button
                        type="button"
                        className="reader-toc-link"
                        data-level={entry.level}
                        data-active={activeId === entry.id}
                        onClick={() => jumpTo(entry.id)}
                      >
                        {entry.text}
                      </button>
                    </li>
                  ))}
                </ul>
              </nav>
            ) : (
              // Keep the grid's first column present so the article stays
              // centered on wide viewports even when there's no TOC.
              <div className="reader-toc" aria-hidden />
            )}

            <article className="reader-article" ref={articleRef}>
              <h1 className="reader-title">{title || 'Untitled'}</h1>
              <div className="reader-meta">
                <span>{minutes} min read</span>
                <span className="reader-meta-dot" aria-hidden />
                <span>Updated {relativeTimeFromSqlite(updatedAt)}</span>
                {sourceLabel ? (
                  <>
                    <span className="reader-meta-dot" aria-hidden />
                    <span className="reader-meta-source">{sourceLabel}</span>
                  </>
                ) : null}
              </div>
              <Suspense fallback={<ReaderBodyFallback />}>
                <MilkdownEditor
                  key={`reader-${pageId}`}
                  defaultValue={body}
                  onChange={noop}
                  ariaLabel="Page body"
                  aliveWikilinkIds={aliveWikilinkIds}
                  collabPageId={null}
                  readOnly
                  wikilinkMode={wikilinkMode}
                  pageId={pageId}
                  onViewReady={handleViewReady}
                />
              </Suspense>
            </article>
          </div>
        </div>
      </div>
    </div>
  )
}

function ReaderBodyFallback() {
  return (
    <div
      role="status"
      aria-busy="true"
      aria-label="Loading page"
      className={cn(
        'min-h-[calc(var(--space-8)*8)]',
        'rounded-[var(--radius-sm)]',
        'bg-[var(--surface-2)]',
      )}
    />
  )
}

function noop() {
  // Read-only — onChange is required by MilkdownEditor's typing but never
  // meaningfully fires (the editable predicate is gated by readOnly).
}
