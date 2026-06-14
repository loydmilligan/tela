import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Download, Play, Presentation } from 'lucide-react'
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
interface DeckOutline {
  count: number
  slides: DeckSlide[]
  features?: { katex?: boolean; mermaid?: boolean; tweet?: boolean; bluesky?: boolean; monaco?: unknown }
  errors?: { row?: number; message: string }[]
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
export function DeckOverview({ spaceId, pageId }: { spaceId: number; pageId: number }) {
  const navigate = useNavigate()

  const { data = null, error } = useQuery({
    queryKey: ['deck-outline', pageId],
    queryFn: () => api<DeckOutline>(`/api/pages/${pageId}/deck/outline`),
    staleTime: 30_000,
    retry: false,
  })
  const err = error ? (error as Error).message || 'Failed to read deck' : null

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
