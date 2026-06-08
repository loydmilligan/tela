import { useMemo, useState } from 'react'
import { useSearch, useNavigate } from '@tanstack/react-router'
import { Sparkles, Send, AlertTriangle, Loader2 } from 'lucide-react'
import { Card, CardBody } from '../ui/card'
import { Button } from '../ui/button'
import { TextArea } from '../ui/textarea'
import { Select } from '../ui/select'
import { EmptyState } from '../ui/empty-state'
import { useSpaces } from '../../lib/queries/spaces'
import { useAskDocs } from '../../lib/queries/ask'
import { navigateToPage } from '../../lib/pageHitItem'
import { ApiError, ASK_UNAVAILABLE_CODES, type SemanticHit } from '../../lib/api'
import { SearchResult } from './SearchResult'

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
  const ask = useAskDocs()

  // Dedupe chunk-level sources to one row per page (hits arrive score-ordered,
  // so the first chunk per page is its strongest citation).
  const sources = useMemo<SemanticHit[]>(() => {
    const hits = ask.data?.sources ?? []
    const seen = new Set<number>()
    const out: SemanticHit[] = []
    for (const h of hits) {
      if (seen.has(h.page_id)) continue
      seen.add(h.page_id)
      out.push(h)
    }
    return out
  }, [ask.data])

  function setScope(value: string) {
    const id = value ? Number(value) : undefined
    void navigate({
      to: '/ask',
      search: () => (id ? { space: id } : {}),
      replace: true,
    })
  }

  function submit() {
    const q = question.trim()
    if (q.length === 0 || ask.isPending) return
    ask.mutate({ question: q, spaceId: scopeSpace })
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    // Enter submits; Shift+Enter inserts a newline (multi-line questions).
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      submit()
    }
  }

  const unavailable = isUnavailable(ask.error)

  return (
    <div className="flex-1 flex flex-col gap-[var(--space-5)] p-[var(--space-7)] max-w-[64rem] w-full mx-auto min-h-0">
      <header className="flex items-center gap-[var(--space-3)]">
        <Sparkles
          aria-hidden
          width={18}
          height={18}
          className="text-[var(--accent)]"
        />
        <h1 className="m-0 text-[length:var(--text-xl)] leading-[var(--leading-tight)] font-[family-name:var(--font-sans)] text-[var(--text-primary)]">
          Ask your docs
        </h1>
      </header>

      <Card>
        <CardBody className="gap-[var(--space-3)]">
          <TextArea
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Ask a question about your pages…"
            aria-label="Question"
            rows={3}
            autoFocus
            disabled={ask.isPending}
          />
          <div className="flex flex-wrap items-center gap-[var(--space-3)]">
            <label
              htmlFor="ask-scope"
              className="text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]"
            >
              Search in
            </label>
            <Select
              id="ask-scope"
              value={scopeSpace != null ? String(scopeSpace) : ''}
              onChange={(e) => setScope(e.target.value)}
              className="max-w-[16rem]"
              aria-label="Limit to a space"
            >
              <option value="">All spaces</option>
              {spaces.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </Select>
            <Button
              variant="primary"
              size="sm"
              className="ml-auto"
              onClick={submit}
              disabled={question.trim().length === 0 || ask.isPending}
            >
              {ask.isPending ? (
                <Loader2 width={16} height={16} className="animate-spin" />
              ) : (
                <Send width={16} height={16} />
              )}
              Ask
            </Button>
          </div>
        </CardBody>
      </Card>

      <section aria-label="Answer" aria-live="polite" className="min-h-0">
        {unavailable ? (
          <EmptyState
            icon={Sparkles}
            title="Ask your docs isn't available yet"
            description="This tela instance hasn't been configured with an AI model for answering questions. Search still works in the meantime."
          />
        ) : ask.isError ? (
          <EmptyState
            icon={AlertTriangle}
            tone="danger"
            title="Couldn't generate an answer"
            description={
              ask.error instanceof ApiError
                ? ask.error.message
                : 'Something went wrong. Try again.'
            }
            actions={
              <Button variant="secondary" size="sm" onClick={submit}>
                Try again
              </Button>
            }
          />
        ) : ask.isPending ? (
          <Card>
            <CardBody className="flex-row items-center gap-[var(--space-3)]">
              <Loader2
                aria-hidden
                width={16}
                height={16}
                className="animate-spin text-[var(--text-muted)]"
              />
              <span className="text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
                Reading your pages and writing an answer…
              </span>
            </CardBody>
          </Card>
        ) : ask.data ? (
          <div className="flex flex-col gap-[var(--space-5)]">
            <Card>
              <CardBody>
                <p className="m-0 whitespace-pre-wrap text-[length:var(--text-base)] leading-[var(--leading-relaxed)] text-[var(--text-primary)] font-[family-name:var(--font-sans)]">
                  {ask.data.answer}
                </p>
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
                  />
                ))}
              </div>
            ) : null}
          </div>
        ) : (
          <p className="m-0 px-[var(--space-4)] py-[var(--space-3)] text-[length:var(--text-sm)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
            Ask a question and get an answer grounded in your pages, with links
            to the sources it used.
          </p>
        )}
      </section>
    </div>
  )
}
