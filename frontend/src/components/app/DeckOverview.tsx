import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Check, Download, Palette, Play, Presentation } from 'lucide-react'
import { api } from '../../lib/api'
import { useUpdatePage } from '../../lib/queries/pages'
import type { Page } from '../../lib/types'
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
export function DeckOverview({ page, spaceId }: { page: Page; spaceId: number }) {
  const pageId = page.id
  const navigate = useNavigate()
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
  const currentVariant = (page.props?.variant as string) || 'editorial'
  const currentLabel = variants?.find((v) => v.name === currentVariant)?.label || 'Editorial'
  const setVariant = (name: string) =>
    void updatePage.mutateAsync({ id: pageId, props: { ...(page.props ?? {}), variant: name } })

  const present = () =>
    void navigate({
      to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
      params: { spaceId, pageId, slug: undefined },
      search: (p) => ({ ...p, view: 'read' }),
    })

  const features = data?.features
    ? featureLabels.filter(([k]) => Boolean(data.features?.[k])).map(([, label]) => label)
    : []

  return (
    <div className="flex flex-col gap-[var(--space-4)]">
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
            <Button variant="ghost" size="sm">
              <Palette width={16} height={16} /> {currentLabel}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {(variants ?? []).map((v) => (
              <DropdownMenuItem key={v.name} onSelect={() => setVariant(v.name)}>
                <Check
                  width={14}
                  height={14}
                  className={v.name === currentVariant ? 'opacity-100' : 'opacity-0'}
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
