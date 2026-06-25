import { useMemo, useState } from 'react'
import { useSearch, useNavigate } from '@tanstack/react-router'
import {
  Sparkles,
  Send,
  AlertTriangle,
  Loader2,
  Library,
  Check,
  ChevronDown,
  ArrowUpRight,
  Wrench,
} from 'lucide-react'
import { Card, CardBody } from '../ui/card'
import { Button } from '../ui/button'
import { TextArea } from '../ui/textarea'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'
import { EmptyState } from '../ui/empty-state'
import { useSpaces } from '../../lib/queries/spaces'
import { useHostContext } from '../../lib/queries/host-context'
import type { Space } from '../../lib/types'
import { useAskDocsStream } from '../../lib/queries/ask'
import { navigateToPage } from '../../lib/pageHitItem'
import { ApiError, ASK_UNAVAILABLE_CODES, type SemanticHit } from '../../lib/api'
import { SearchResult } from './SearchResult'
import { usePageHoverPreview } from './wikilink-hover-preview'
import { MarkdownView } from '../view/MarkdownView'

interface AskSearchParams {
  space?: number
}

// Whether an error is the backend's "feature not configured on this instance"
// 503 (no embedder or no managed-AI model) rather than a real failure. Surfaced
// as a calm unavailable state, never an error toast.
function isUnavailable(err: unknown): boolean {
  return (
    err instanceof ApiError &&
    err.status === 503 &&
    (ASK_UNAVAILABLE_CODES as readonly string[]).includes(err.code)
  )
}

// isModelUnreachable — the failure is the answer model being cold / unreachable /
// timing out, not a configuration gap: a network error (no HTTP response, status
// 0), an upstream gateway error (502/504), or the backend's "generation failed".
// These are transient — a retry usually works once the model has warmed up.
function isModelUnreachable(err: unknown): boolean {
  return (
    err instanceof ApiError &&
    (err.status === 0 ||
      err.status === 502 ||
      err.status === 504 ||
      err.code === 'completion_failed')
  )
}

function askErrorMessage(err: unknown): string {
  if (isModelUnreachable(err)) {
    return 'The model may be warming up or briefly offline. Give it a moment and try again.'
  }
  if (err instanceof ApiError) return err.message
  return 'Something went wrong. Try again.'
}

// /ask — "Ask your docs". A question box → an LLM answer grounded in the user's
// pages (POST /api/rag/ask), followed by the cited source pages as clickable
// rows. Mirrors the SearchRoute shell (header + Card + result list) and reuses
// the SearchResult row for citations. The optional `?space=` scopes retrieval
// to one space; default is all readable spaces.
//
// Yjs scope (Hard Rule #6): zero Yjs imports here.
export function AskRoute() {
  const params = useSearch({ from: '/_app/ask' }) as AskSearchParams
  const navigate = useNavigate()
  const spacesQuery = useSpaces()
  const spaces = useMemo(() => spacesQuery.data ?? [], [spacesQuery.data])

  const scopeSpace =
    typeof params.space === 'number' ? params.space : undefined

  const [question, setQuestion] = useState('')
  const ask = useAskDocsStream()
  const preview = usePageHoverPreview()
  // AI off (admin kill-switch or embedder unconfigured) → don't let the user fire
  // a request that just 503s; show a clear notice instead.
  const host = useHostContext().data
  const aiPaused = host ? !host.ai_available : false
  const maintenanceNote = host?.maintenance?.notice?.trim()

  // Dedupe chunk-level sources to one row per page (hits arrive score-ordered,
  // so the first chunk per page is its strongest citation).
  const sources = useMemo<SemanticHit[]>(() => {
    const seen = new Set<number>()
    const out: SemanticHit[] = []
    for (const h of ask.sources) {
      if (seen.has(h.page_id)) continue
      seen.add(h.page_id)
      out.push(h)
    }
    return out
  }, [ask.sources])

  function setScope(value: string) {
    const id = value ? Number(value) : undefined
    void navigate({
      to: '/ask',
      search: () => (id ? { space: id } : {}),
      replace: true,
    })
  }

  function runQuestion(q: string) {
    const trimmed = q.trim()
    if (trimmed.length === 0 || ask.isPending) return
    setQuestion(trimmed)
    ask.ask(trimmed, scopeSpace)
  }

  function submit() {
    runQuestion(question)
  }

  // Starter prompts for the empty state — clicking runs the question. Generic on
  // purpose so they work on any instance regardless of content.
  const SUGGESTIONS = [
    'What changed recently?',
    'How do I get started?',
    'Summarize what’s in my spaces',
  ]

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    // Enter submits; Shift+Enter inserts a newline (multi-line questions).
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      submit()
    }
  }

  const unavailable = isUnavailable(ask.error)
  // The answer area appears the moment retrieval returns sources or the first
  // token lands, and persists once done — so a finished answer with zero sources
  // still shows.
  const showAnswer =
    ask.status === 'done' || ask.answer.length > 0 || sources.length > 0
  // Retrieval phase: streaming has started but nothing has come back yet.
  const retrieving =
    ask.isPending && ask.answer.length === 0 && sources.length === 0

  return (
    <div className="flex-1 flex flex-col gap-[var(--space-6)] px-[var(--space-7)] pt-[calc(var(--space-8)*1.5)] pb-[var(--space-7)] max-w-[42rem] w-full mx-auto min-h-0">
      <header className="flex flex-col gap-[var(--space-2)]">
        <div className="flex items-center gap-[var(--space-3)]">
          <span
            aria-hidden
            className="flex items-center justify-center size-[var(--space-8)] shrink-0 rounded-[var(--radius-md)] bg-[var(--accent)] text-[var(--accent-fg)] shadow-[var(--shadow-sm)]"
          >
            <Sparkles width={17} height={17} />
          </span>
          <h1 className="m-0 text-[length:var(--text-2xl)] leading-[var(--leading-tight)] tracking-[-0.02em] font-semibold font-[family-name:var(--font-sans)] text-[var(--text-primary)]">
            Ask your docs
          </h1>
        </div>
        <p className="m-0 text-[length:var(--text-sm)] leading-[var(--leading-normal)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
          Answers grounded in your pages, with the exact sources they came from.
        </p>
      </header>

      {/* One composer surface: the textarea blends into the card (its own border
          removed) and the whole card lights up on focus, so it reads as a single
          input rather than a box-in-a-box. */}
      <Card className="bg-[var(--surface-1)] rounded-[var(--radius-lg)] shadow-[var(--shadow-md)] transition-[border-color,box-shadow] duration-[var(--duration-fast)] focus-within:border-[var(--accent)] focus-within:ring-2 focus-within:ring-[color-mix(in_oklch,var(--accent)_30%,transparent)]">
        <CardBody className="gap-[var(--space-3)] p-[var(--space-4)]">
          <TextArea
            font="sans"
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={aiPaused ? 'Ask is temporarily unavailable…' : 'Ask anything about your pages…'}
            aria-label="Question"
            rows={2}
            autoFocus
            disabled={ask.isPending || aiPaused}
            className="border-0 bg-transparent resize-none px-0 py-[var(--space-1)] text-[length:var(--text-base)] min-h-[calc(var(--space-8)*1.75)] placeholder:text-[var(--text-muted)] focus-visible:ring-0 focus-visible:ring-offset-0 focus-visible:border-transparent"
          />
          <div className="flex items-center gap-[var(--space-2)]">
            <ScopePicker
              spaces={spaces}
              value={scopeSpace}
              onChange={(id) => setScope(id != null ? String(id) : '')}
            />
            <span className="ml-auto flex items-center gap-[var(--space-1)] whitespace-nowrap text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)] hidden sm:flex">
              <Kbd>↵</Kbd> to ask
            </span>
            {/* Circular send — a focused "ask" affordance, not a generic form
                button. Owned Button primitive, shaped to a circle. */}
            <Button
              variant="primary"
              size="sm"
              onClick={submit}
              aria-label="Ask"
              disabled={question.trim().length === 0 || ask.isPending || aiPaused}
              className="size-[var(--space-8)] shrink-0 rounded-full p-0"
            >
              {ask.isPending ? (
                <Loader2 width={16} height={16} className="animate-spin" />
              ) : (
                <Send width={16} height={16} />
              )}
            </Button>
          </div>
        </CardBody>
      </Card>

      <section aria-label="Answer" aria-live="polite" className="min-h-0">
        {aiPaused ? (
          <EmptyState
            icon={Wrench}
            title="AI is temporarily unavailable"
            description={
              maintenanceNote ||
              'AI features (ask & semantic search) are offline right now — the model may be down or under maintenance. Full-text search still works; give it a moment and try again later.'
            }
          />
        ) : unavailable ? (
          <EmptyState
            icon={Sparkles}
            title="Ask your docs isn't available yet"
            description="This tela instance hasn't been configured with an AI model for answering questions. Search still works in the meantime."
          />
        ) : ask.status === 'error' ? (
          <EmptyState
            icon={AlertTriangle}
            tone="danger"
            title={isModelUnreachable(ask.error) ? 'The answer model didn’t respond' : 'Couldn’t generate an answer'}
            description={askErrorMessage(ask.error)}
            actions={
              <Button variant="secondary" size="sm" onClick={submit}>
                Try again
              </Button>
            }
          />
        ) : retrieving ? (
          <Card>
            <CardBody className="flex-row items-center gap-[var(--space-3)]">
              <Loader2
                aria-hidden
                width={16}
                height={16}
                className="animate-spin text-[var(--text-muted)]"
              />
              <span className="text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
                Reading your pages…
              </span>
            </CardBody>
          </Card>
        ) : showAnswer ? (
          <div className="flex flex-col gap-[var(--space-5)]">
            <Card>
              <CardBody>
                {/* Answer streams in as markdown — render through the shared
                    read-view renderer, with a live caret while still writing. */}
                {ask.answer ? <MarkdownView body={ask.answer} /> : null}
                {ask.isPending ? (
                  ask.answer ? (
                    <span
                      aria-hidden
                      className="inline-block ml-[2px] w-[2px] h-[1.1em] align-text-bottom bg-[var(--accent)] animate-pulse"
                    />
                  ) : (
                    <span className="inline-flex items-center gap-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
                      <Loader2 width={14} height={14} className="animate-spin" />
                      Writing…
                    </span>
                  )
                ) : null}
              </CardBody>
            </Card>
            {sources.length > 0 ? (
              <div className="flex flex-col gap-[var(--space-1)]">
                <h2 className="m-0 px-[var(--space-4)] text-[length:var(--text-xs)] uppercase tracking-[0.04em] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
                  Sources
                </h2>
                {sources.map((h) => (
                  <SearchResult
                    key={h.page_id}
                    title={h.title}
                    breadcrumb={h.heading_path}
                    excerpt={h.snippet}
                    updatedAt={h.updated_at}
                    onSelect={() => navigateToPage(h.space_id, h.page_id)}
                    hoverProps={preview.triggerProps(h.page_id, h.title)}
                  />
                ))}
                {preview.card}
              </div>
            ) : null}
            {ask.status === 'done' && ask.followups.length > 0 ? (
              <FollowUps
                questions={ask.followups}
                onAsk={runQuestion}
                disabled={ask.isPending}
              />
            ) : null}
          </div>
        ) : (
          <div className="flex flex-col gap-[var(--space-3)]">
            <span className="text-[length:var(--text-xs)] uppercase tracking-[0.06em] text-[var(--text-muted)] font-medium font-[family-name:var(--font-sans)]">
              Try asking
            </span>
            <div className="flex flex-col gap-[var(--space-1)]">
              {SUGGESTIONS.map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => runQuestion(s)}
                  className="group flex items-center justify-between gap-[var(--space-3)] w-full text-left rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-1)] px-[var(--space-3)] py-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-primary)] font-[family-name:var(--font-sans)] cursor-pointer transition-[background-color,border-color] duration-[var(--duration-fast)] hover:bg-[var(--surface-2)] hover:border-[color-mix(in_oklch,var(--accent)_45%,var(--border-subtle))] outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)]"
                >
                  <span className="truncate">{s}</span>
                  <ArrowUpRight
                    width={15}
                    height={15}
                    aria-hidden
                    className="shrink-0 text-[var(--text-muted)] opacity-50 transition-[opacity,color,transform] duration-[var(--duration-fast)] group-hover:opacity-100 group-hover:text-[var(--accent)] group-hover:-translate-y-[1px] group-hover:translate-x-[1px]"
                  />
                </button>
              ))}
            </div>
          </div>
        )}
      </section>
    </div>
  )
}

// FollowUps — the ask-first navigation thread. The backend suggests up to 3 next
// questions per answer; clicking one runs it, so an answer becomes something to
// pull on rather than a dead end. Same row language as the empty-state starters.
function FollowUps({
  questions,
  onAsk,
  disabled,
}: {
  questions: string[]
  onAsk: (q: string) => void
  disabled: boolean
}) {
  return (
    <div className="flex flex-col gap-[var(--space-2)]">
      <h2 className="m-0 px-[var(--space-4)] text-[length:var(--text-xs)] uppercase tracking-[0.04em] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
        Keep exploring
      </h2>
      <div className="flex flex-col gap-[var(--space-1)]">
        {questions.map((q) => (
          <button
            key={q}
            type="button"
            disabled={disabled}
            onClick={() => onAsk(q)}
            className="group flex items-center justify-between gap-[var(--space-3)] w-full text-left rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-1)] px-[var(--space-3)] py-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-primary)] font-[family-name:var(--font-sans)] cursor-pointer transition-[background-color,border-color] duration-[var(--duration-fast)] hover:bg-[var(--surface-2)] hover:border-[color-mix(in_oklch,var(--accent)_45%,var(--border-subtle))] outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)] disabled:cursor-not-allowed disabled:opacity-60"
          >
            <span className="truncate">{q}</span>
            <ArrowUpRight
              width={15}
              height={15}
              aria-hidden
              className="shrink-0 text-[var(--text-muted)] opacity-50 transition-[opacity,color,transform] duration-[var(--duration-fast)] group-hover:opacity-100 group-hover:text-[var(--accent)] group-hover:-translate-y-[1px] group-hover:translate-x-[1px]"
            />
          </button>
        ))}
      </div>
    </div>
  )
}

// Kbd — a small keycap for shortcut hints. The bordered-keycap detail reads as a
// real tool (Linear/Raycast) where plain text reads generic.
function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <kbd className="inline-flex items-center justify-center min-w-[var(--space-5)] h-[var(--space-5)] px-[var(--space-1)] rounded-[var(--radius-sm)] border border-[var(--border-subtle)] bg-[var(--surface-2)] text-[length:var(--text-xs)] leading-none text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
      {children}
    </kbd>
  )
}

// ScopePicker — the "answer from" control. A compact, auto-width dropdown (owned
// Radix menu) rather than a native <select>, so it reads as a deliberate filter
// instead of a stranded-chevron form box.
function ScopePicker({
  spaces,
  value,
  onChange,
}: {
  spaces: Space[]
  value: number | undefined
  onChange: (id: number | undefined) => void
}) {
  const current = value != null ? spaces.find((s) => s.id === value) : undefined
  const label = current ? current.name : 'All spaces'

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={`Answer from: ${label}`}
          className="inline-flex max-w-[14rem] items-center gap-[var(--space-1)] rounded-[var(--radius-md)] px-[var(--space-2)] py-[var(--space-1)] text-[length:var(--text-sm)] text-[var(--text-primary)] font-[family-name:var(--font-sans)] cursor-pointer outline-none transition-colors duration-[var(--duration-fast)] hover:bg-[var(--surface-3)] focus-visible:ring-2 focus-visible:ring-[var(--accent)] data-[state=open]:bg-[var(--surface-3)]"
        >
          <Library
            width={14}
            height={14}
            aria-hidden
            className="shrink-0 text-[var(--text-muted)]"
          />
          <span className="truncate">{label}</span>
          <ChevronDown
            width={14}
            height={14}
            aria-hidden
            className="shrink-0 text-[var(--text-muted)]"
          />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-[18rem] overflow-y-auto">
        <ScopeOption
          label="All spaces"
          icon={<Library width={14} height={14} aria-hidden className="text-[var(--text-muted)]" />}
          selected={value == null}
          onSelect={() => onChange(undefined)}
        />
        {spaces.map((s) => (
          <ScopeOption
            key={s.id}
            label={s.name || 'Untitled space'}
            selected={value === s.id}
            onSelect={() => onChange(s.id)}
          />
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function ScopeOption({
  label,
  icon,
  selected,
  onSelect,
}: {
  label: string
  icon?: React.ReactNode
  selected: boolean
  onSelect: () => void
}) {
  return (
    <DropdownMenuItem onSelect={onSelect} className="gap-[var(--space-2)] pr-[var(--space-6)]">
      <span className="flex w-[var(--space-4)] shrink-0 items-center justify-center">
        {selected ? (
          <Check width={14} height={14} aria-hidden className="text-[var(--accent)]" />
        ) : (
          icon ?? null
        )}
      </span>
      <span className="truncate">{label}</span>
    </DropdownMenuItem>
  )
}
