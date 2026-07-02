import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Check, Download, Palette, Play, Presentation } from 'lucide-react'
import { api } from '../../lib/api'
import { useUpdatePage } from '../../lib/queries/pages'
import type { Page } from '../../lib/types'
import { DeckCoverImage } from './deck-cover-image'
import { useFileDownload } from './use-file-download'
import { toast, updateToast } from '../ui/toast'
import { Button } from '../ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'

interface DeckSlide {
  no: number
  title: string
  layout: string
  note: string
}
interface DeckOutline {
  count: number
  slides: DeckSlide[]
  features?: { katex?: boolean; mermaid?: boolean; tweet?: boolean; bluesky?: boolean; monaco?: unknown }
  errors?: { row?: number; message: string }[]
}
interface DeckVariant {
  name: string
  label: string
  scheme: string
  description: string
}

const featureLabels: [keyof NonNullable<DeckOutline['features']>, string][] = [
  ['katex', 'Math'],
  ['mermaid', 'Diagrams'],
  ['monaco', 'Live code'],
  ['tweet', 'Tweets'],
  ['bluesky', 'Bluesky'],
]

// The deck's default (non-present) view. Instead of dumping raw Slidev markdown
// as prose, show the deck's identity: slide count, a Present CTA, exports, and a
// parsed outline (title + layout per slide). Cheap — backed by /parse, no render.
export function DeckOverview({ page }: { page: Page }) {
  const pageId = page.id
  const updatePage = useUpdatePage()

  const { data = null, error } = useQuery({
    queryKey: ['deck-outline', pageId],
    queryFn: () => api<DeckOutline>(`/api/pages/${pageId}/deck/outline`),
    staleTime: 30_000,
    retry: false,
  })
  const err = error ? (error as Error).message || 'Failed to read deck' : null

  // Style variants come from the theme package (slidev-theme-tahta) — tela
  // hardcodes none. Picking one writes props.variant (merged, PUT semantics).
  const { data: variants } = useQuery({
    queryKey: ['deck-variants'],
    queryFn: () => api<DeckVariant[]>('/api/deck/themes'),
    staleTime: Infinity,
    retry: false,
  })
  // The variant is a deliberate choice, not a default — an unset deck shows
  // "Choose a style", never a silent Editorial masquerading as chosen.
  const chosenVariant = (page.props?.variant as string) || ''
  const chosenLabel = variants?.find((v) => v.name === chosenVariant)?.label
  const setVariant = (name: string) =>
    void updatePage.mutateAsync({ id: pageId, props: { ...(page.props ?? {}), variant: name } })

  // Present = the live Slidev SPA (real presenter/overview/drawing) in a new tab.
  // Same-origin → the session cookie carries RBAC.
  const present = () => window.open(`/api/pages/${pageId}/deck/spa/`, '_blank', 'noopener')

  // Export renders headless Chromium frames (several seconds) — drive a fetch +
  // toast so the menu gives feedback instead of a silent new tab that hangs.
  const { download: downloadPdf } = useFileDownload(`/api/pages/${pageId}/deck.pdf`)
  const { download: downloadPptx } = useFileDownload(
    `/api/pages/${pageId}/deck.pptx`,
    { fallbackName: 'deck.pptx' },
  )
  // Raw Slidev source — the deck body verbatim, directly runnable with
  // `slidev slides.md`. Instant (no headless render), so no toast dance.
  const { download: downloadMd } = useFileDownload(
    `/api/pages/${pageId}/deck.md`,
    { fallbackName: 'deck.md' },
  )
  const exportDeck = (kind: 'PDF' | 'PPTX') => {
    const id = toast({ title: `Preparing ${kind}…`, loading: true, duration: 0 })
    void (kind === 'PDF' ? downloadPdf() : downloadPptx()).then((ok) =>
      updateToast(
        id,
        ok
          ? {
              title: `${kind} ready`,
              description: 'Your download has started.',
              variant: 'success',
              loading: false,
              duration: 4000,
            }
          : {
              title: `${kind} export failed`,
              description: 'Please try again.',
              variant: 'destructive',
              loading: false,
              duration: 6000,
            },
      ),
    )
  }

  const features = data?.features
    ? featureLabels.filter(([k]) => Boolean(data.features?.[k])).map(([, label]) => label)
    : []

  return (
    <div className="flex flex-col gap-[var(--space-4)]">
      {/* First-slide cover — keyed by variant so switching restyle re-fetches and
          resets the error state. Until a style is chosen, prompt for the decision
          instead of previewing a default the author never picked. */}
      {chosenVariant ? (
        <DeckCover key={chosenVariant} pageId={pageId} variant={chosenVariant} onPresent={present} />
      ) : (
        <div className="flex flex-col items-center justify-center gap-[var(--space-2)] rounded-[var(--radius-md)] border border-dashed border-[var(--border-default)] bg-[var(--surface-2)] px-[var(--space-4)] py-[var(--space-7)] text-center">
          <Palette width={20} height={20} className="text-[var(--text-muted)]" />
          <div className="font-medium text-[var(--text-primary)]">Choose a style</div>
          <div className="max-w-sm text-[var(--text-sm)] text-[var(--text-muted)]">
            Pick the variant that fits this deck — it sets the typeface, color scheme, and texture. There's no default; it's a deliberate choice.
          </div>
        </div>
      )}
      <div className="flex flex-wrap items-center gap-[var(--space-3)] rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-2)] p-[var(--space-4)]">
        <Presentation width={20} height={20} className="text-[var(--text-muted)]" />
        <div className="mr-auto">
          <div className="font-medium text-[var(--text-primary)]">Slide deck</div>
          <div className="text-[var(--text-sm)] text-[var(--text-muted)]">
            {data ? `${data.count} slide${data.count === 1 ? '' : 's'}` : err ? 'Deck unavailable' : 'Reading…'}
            {features.length ? ` · ${features.join(' · ')}` : ''}
          </div>
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant={chosenVariant ? 'ghost' : 'secondary'} size="sm">
              <Palette width={16} height={16} /> {chosenLabel ?? 'Choose a style'}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {(variants ?? []).map((v) => (
              <DropdownMenuItem key={v.name} onSelect={() => setVariant(v.name)}>
                <Check
                  width={14}
                  height={14}
                  className={v.name === chosenVariant ? 'opacity-100' : 'opacity-0'}
                />
                <span className="ml-[var(--space-1)]">{v.label}</span>
                <span className="ml-auto pl-[var(--space-3)] text-[var(--text-xs)] text-[var(--text-muted)]">{v.scheme}</span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        <Button variant="primary" size="sm" onClick={present}>
          <Play width={16} height={16} /> Present
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="sm" aria-label="Export deck">
              <Download width={16} height={16} />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={() => exportDeck('PDF')}>
              Download PDF
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => exportDeck('PPTX')}>
              Download PPTX
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => void downloadMd()}>
              Slidev source (.md)
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {data && data.slides.length ? (
        <ol className="flex flex-col gap-[var(--space-1)]">
          {data.slides.map((s) => (
            <li
              key={s.no}
              className="flex items-center gap-[var(--space-3)] rounded-[var(--radius-sm)] px-[var(--space-3)] py-[var(--space-2)] hover:bg-[var(--surface-2)]"
            >
              <span className="w-6 text-right text-[var(--text-sm)] tabular-nums text-[var(--text-muted)]">{s.no}</span>
              <span className="min-w-0 flex-1 truncate text-[var(--text-primary)]">{s.title || <span className="text-[var(--text-muted)]">Untitled slide</span>}</span>
              <span className="rounded-[var(--radius-sm)] bg-[var(--surface-3)] px-[var(--space-2)] py-[2px] text-[var(--text-xs)] text-[var(--text-muted)]">
                {s.layout}
              </span>
            </li>
          ))}
        </ol>
      ) : null}
    </div>
  )
}

// The deck's first-slide cover, clickable to present. Renders lazily server-side
// (the gated cover route renders-if-needed); DeckCoverImage shows a skeleton and
// retries a cold/queued render before hiding so a transient miss never strands a
// cover that's actually ready. Remounted (keyed) per variant by the caller.
function DeckCover({
  pageId,
  variant,
  onPresent,
}: {
  pageId: number
  variant: string
  onPresent: () => void
}) {
  const [ok, setOk] = useState(true)
  if (!ok) return null
  return (
    <button
      type="button"
      onClick={onPresent}
      aria-label="Present"
      className="group relative block aspect-video w-full max-w-2xl overflow-hidden rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-2)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
    >
      <DeckCoverImage
        src={`/api/pages/${pageId}/deck/cover?v=${encodeURIComponent(variant)}`}
        loading="lazy"
        onGiveUp={() => setOk(false)}
        className="size-full object-cover"
      />
      <span className="absolute inset-0 flex items-center justify-center bg-[color-mix(in_srgb,var(--text-primary)_40%,transparent)] opacity-0 transition-opacity duration-[var(--duration-fast)] group-hover:opacity-100">
        <span className="flex items-center gap-[var(--space-2)] rounded-[var(--radius-md)] bg-[var(--surface-1)] px-[var(--space-3)] py-[var(--space-2)] text-[length:var(--text-sm)] font-medium shadow-[var(--shadow-md)]">
          <Play width={16} height={16} /> Present
        </span>
      </span>
    </button>
  )
}
