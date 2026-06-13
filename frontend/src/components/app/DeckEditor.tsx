import { useEffect, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Download, Play, X } from 'lucide-react'
import { api } from '../../lib/api'
import { useUpdatePage } from '../../lib/queries/pages'
import { Button } from '../ui/button'
import { Select } from '../ui/select'
import type { Page } from '../../lib/types'

interface ThemeOpt {
  name: string
  label: string
}

// Deck pages edit as PLAIN markdown (Slidev markdown) — not the Milkdown rich
// editor (a deck is a separate content model). A theme selector writes the
// `theme` prop; the body saves to the same PATCH /api/pages/{id} path. Present
// (read mode) renders it via the deck sidecar — see DeckPresenter.
export function DeckEditor({
  page,
  spaceId,
}: {
  page: Page
  spaceId: number
  onDeleted: () => void
}) {
  const navigate = useNavigate()
  const updatePage = useUpdatePage()
  const [body, setBody] = useState(page.body)
  const [themes, setThemes] = useState<ThemeOpt[]>([])
  const theme = (page.props?.theme as string | undefined) ?? ''
  const saveTimer = useRef<number | null>(null)

  useEffect(() => {
    let alive = true
    api<ThemeOpt[]>('/api/deck/themes')
      .then((t) => alive && setThemes(t))
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [])

  const save = (next: string) => {
    if (next === page.body) return
    void updatePage.mutateAsync({ id: page.id, body: next })
  }
  const onChange = (v: string) => {
    setBody(v)
    if (saveTimer.current) window.clearTimeout(saveTimer.current)
    saveTimer.current = window.setTimeout(() => save(v), 800)
  }
  const setTheme = (t: string) => {
    void updatePage.mutateAsync({
      id: page.id,
      props: { ...(page.props ?? {}), deck: true, theme: t },
    })
  }
  const present = () =>
    void navigate({
      to: '/spaces/$spaceId/pages/$pageId/{-$slug}',
      params: { spaceId, pageId: page.id, slug: undefined },
      search: (p) => ({ ...p, edit: undefined, view: 'read' as const }),
    })
  // The deck editor IS the page's in-shell home (default view), so there's no
  // intermediate read state to "close" to — backing out leaves for the space.
  const exit = () => void navigate({ to: '/spaces/$spaceId', params: { spaceId } })

  return (
    <div className="flex h-full w-full flex-col bg-[var(--surface-1)]">
      <header className="flex items-center justify-between gap-[var(--space-4)] border-b border-[var(--border-subtle)] px-[var(--space-5)] py-[var(--space-3)]">
        <div className="flex min-w-0 items-center gap-[var(--space-3)]">
          <Button variant="ghost" size="sm" onClick={exit} aria-label="Close editor">
            <X width={16} height={16} />
          </Button>
          <span className="rounded-[var(--radius-sm)] bg-[var(--surface-2)] px-[var(--space-2)] py-[2px] text-[var(--text-xs)] font-medium uppercase tracking-wide text-[var(--text-muted)]">
            Deck
          </span>
          <span className="truncate font-medium">{page.title}</span>
        </div>
        <div className="flex flex-shrink-0 items-center gap-[var(--space-2)]">
          <Select
            value={theme}
            onChange={(e) => setTheme(e.target.value)}
            className="w-auto"
            aria-label="Deck theme"
          >
            <option value="">Default theme</option>
            {themes.map((t) => (
              <option key={t.name} value={t.name}>
                {t.label}
              </option>
            ))}
          </Select>
          <a href={`/api/pages/${page.id}/deck.pdf`} target="_blank" rel="noreferrer">
            <Button variant="ghost" size="sm">
              <Download width={16} height={16} />
              <span className="ml-[var(--space-1)]">PDF</span>
            </Button>
          </a>
          <Button variant="primary" size="sm" onClick={present}>
            <Play width={16} height={16} />
            <span className="ml-[var(--space-1)]">Present</span>
          </Button>
        </div>
      </header>
      <textarea
        value={body}
        onChange={(e) => onChange(e.target.value)}
        onBlur={() => save(body)}
        spellCheck={false}
        aria-label="Deck markdown"
        className="flex-1 resize-none bg-[var(--surface-1)] p-[var(--space-6)] font-mono text-[var(--text-sm)] leading-relaxed text-[var(--text-primary)] outline-none"
        placeholder={'# Slide one\n\nWrite slides in Markdown.\n\n---\n\n# Slide two\n\n- Separate slides with ---'}
      />
    </div>
  )
}
