import { useState } from 'react'
import { Check } from 'lucide-react'
import { cn } from '../../lib/utils'
import { Avatar, type AvatarTone } from '../ui/avatar'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '../ui/popover'

// PollWidget — the reader-side surface of a `:::poll` block. Two modes on one
// component: CHOOSE (before you vote — options only, deliberately no results)
// and RESULTS (after your vote lands, or when the poll is closed / read-only).
// Presentational: it renders `PollData` and calls back on intent; the vote op
// (structured body edit, see docs) owns persistence. Tokens + owned primitives
// only — the result bars are a local token element because `Progress` carries
// usage-escalation semantics that are wrong for a poll (winner = accent, the
// rest = muted; never "danger when full").

export interface PollVoter {
  id: number
  name: string
  handle?: string
}

export interface PollOption {
  id: string
  label: string
  /** Empty when secret (roster withheld) — `count` still drives the bar. */
  voters: PollVoter[]
  count: number
}

export interface PollData {
  id: string
  question: string
  options: PollOption[]
  /** Option id the caller voted for, or null if they haven't. */
  myChoice: string | null
  /** Anonymous — counts + bars, no rosters, no faces. */
  secret?: boolean
  /** Voting is over — always render read-only results. */
  closed?: boolean
  /** Re-voting allowed while open. */
  allowChange?: boolean
  /** Caller may cast (editor access + open). Non-voters see read-only results. */
  canVote?: boolean
}

export interface PollWidgetProps {
  poll: PollData
  onVote?: (optionId: string) => void
  className?: string
}

const TONES: AvatarTone[] = [
  'collab-1',
  'collab-2',
  'collab-3',
  'collab-4',
  'collab-5',
  'collab-6',
  'collab-7',
  'collab-8',
]

function toneFor(id: number): AvatarTone {
  return TONES[((id % TONES.length) + TONES.length) % TONES.length]
}

function initials(name: string): string {
  const parts = name.trim().split(/\s+/)
  const first = parts[0]?.[0] ?? '?'
  const second = parts.length > 1 ? parts[parts.length - 1][0] : ''
  return (first + second).slice(0, 2)
}

function totalVotes(poll: PollData): number {
  return poll.options.reduce((n, o) => n + o.count, 0)
}

function VoterFace({ voter, className }: { voter: PollVoter; className?: string }) {
  return (
    <Avatar
      size="sm"
      tone={toneFor(voter.id)}
      title={voter.name}
      className={cn('ring-2 ring-[var(--surface-1)]', className)}
    >
      {initials(voter.name)}
    </Avatar>
  )
}

// A compact overlapped stack of the first faces + a "+k" remainder chip that
// opens the full roster for that option.
function AvatarCluster({
  voters,
  count,
  max = 3,
}: {
  voters: PollVoter[]
  count: number
  max?: number
}) {
  if (count === 0) return null
  const shown = voters.slice(0, max)
  const rest = count - shown.length
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="flex items-center pl-[var(--space-2)] transition-opacity hover:opacity-80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)] rounded-full"
          aria-label={`See who voted (${count})`}
        >
          {shown.map((v) => (
            <span key={v.id} className="-ml-[var(--space-2)] first:ml-0">
              <VoterFace voter={v} />
            </span>
          ))}
          {rest > 0 && (
            <span className="-ml-[var(--space-2)] inline-flex h-[var(--space-6)] items-center rounded-full border border-[var(--border-subtle)] bg-[var(--surface-2)] px-[var(--space-2)] text-[length:var(--text-xs)] font-medium text-[var(--text-muted)] ring-2 ring-[var(--surface-1)]">
              +{rest}
            </span>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-[calc(var(--space-8)*4)] p-[var(--space-2)]">
        <VoterRoster voters={voters} count={count} />
      </PopoverContent>
    </Popover>
  )
}

function VoterRoster({ voters, count }: { voters: PollVoter[]; count: number }) {
  return (
    <div className="flex flex-col gap-[var(--space-1)]">
      <div className="px-[var(--space-1)] pb-[var(--space-1)] text-[length:var(--text-xs)] font-medium uppercase tracking-wide text-[var(--text-muted)]">
        {count} vote{count === 1 ? '' : 's'}
      </div>
      {voters.map((v) => (
        <div
          key={v.id}
          className="flex items-center gap-[var(--space-2)] rounded-[var(--radius-sm)] px-[var(--space-1)] py-[var(--space-1)]"
        >
          <VoterFace voter={v} className="ring-0" />
          <span className="truncate text-[length:var(--text-sm)] text-[var(--text-primary)]">
            {v.name}
          </span>
        </div>
      ))}
    </div>
  )
}

function ResultRow({
  option,
  pct,
  mine,
  secret,
}: {
  option: PollOption
  pct: number
  mine: boolean
  secret: boolean
}) {
  return (
    <div className="flex flex-col gap-[var(--space-1)]">
      <div className="flex items-baseline justify-between gap-[var(--space-2)]">
        <span className="flex items-center gap-[var(--space-2)] text-[length:var(--text-sm)] text-[var(--text-primary)]">
          {option.label}
          {mine && (
            <span className="inline-flex items-center gap-[calc(var(--space-1)/2)] text-[length:var(--text-xs)] font-medium text-[var(--accent)]">
              <Check className="h-[var(--space-3)] w-[var(--space-3)]" strokeWidth={2.5} />
              Your vote
            </span>
          )}
        </span>
        <span className="shrink-0 text-[length:var(--text-xs)] tabular-nums text-[var(--text-muted)]">
          {pct}% · {option.count}
        </span>
      </div>
      <div className="flex items-center gap-[var(--space-3)]">
        {/* Result bar — local token element (see file header). */}
        <div className="h-[var(--space-2)] flex-1 overflow-hidden rounded-[var(--radius-sm)] bg-[var(--surface-3)]">
          <div
            className={cn(
              'h-full rounded-[var(--radius-sm)] transition-[width] duration-[var(--duration-base)] ease-[var(--ease-out)]',
              mine
                ? 'bg-[var(--accent)]'
                : 'bg-[color-mix(in_srgb,var(--text-muted)_40%,transparent)]',
            )}
            style={{ width: `${pct}%` }}
          />
        </div>
        {!secret && <AvatarCluster voters={option.voters} count={option.count} />}
      </div>
    </div>
  )
}

function ChooseRow({
  option,
  onVote,
  disabled,
}: {
  option: PollOption
  onVote?: (id: string) => void
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={() => onVote?.(option.id)}
      className="group flex w-full items-center gap-[var(--space-3)] px-[var(--space-3)] py-[var(--space-3)] text-left transition-colors first:rounded-t-[var(--radius-md)] last:rounded-b-[var(--radius-md)] hover:bg-[var(--surface-2)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[var(--accent)] disabled:cursor-not-allowed disabled:opacity-60"
    >
      <span className="h-[var(--space-4)] w-[var(--space-4)] shrink-0 rounded-full border border-[var(--border-strong)] transition-colors group-hover:border-[var(--accent)]" />
      <span className="text-[length:var(--text-sm)] text-[var(--text-primary)]">
        {option.label}
      </span>
    </button>
  )
}

export function PollWidget({ poll, onVote, className }: PollWidgetProps) {
  const [showVotes, setShowVotes] = useState(false)
  const total = totalVotes(poll)
  const voted = poll.myChoice != null
  const readOnly = poll.closed || poll.canVote === false
  // CHOOSE only when the caller can still cast and hasn't. Everything else
  // (voted, closed, no-access) shows results.
  const mode: 'choose' | 'results' = !voted && !readOnly ? 'choose' : 'results'

  return (
    <div
      className={cn(
        'flex flex-col gap-[var(--space-4)] rounded-[var(--radius-md)] border border-[var(--border-subtle)] bg-[var(--surface-1)] p-[var(--space-4)]',
        className,
      )}
    >
      <div className="flex items-start justify-between gap-[var(--space-3)]">
        <div className="flex flex-col gap-[calc(var(--space-1)/2)]">
          <h3 className="text-[length:var(--text-base)] font-semibold text-[var(--text-primary)]">
            {poll.question}
          </h3>
          <p className="text-[length:var(--text-xs)] text-[var(--text-muted)]">
            {mode === 'choose'
              ? 'Choose one'
              : `${total} vote${total === 1 ? '' : 's'}`}
            {poll.secret && ' · anonymous'}
          </p>
        </div>
        <Badge variant={poll.closed ? 'muted' : 'accent'}>
          {poll.closed ? 'Closed' : 'Open'}
        </Badge>
      </div>

      {mode === 'choose' ? (
        <div className="divide-y divide-[var(--border-subtle)] overflow-hidden rounded-[var(--radius-md)] border border-[var(--border-subtle)]">
          {poll.options.map((o) => (
            <ChooseRow key={o.id} option={o} onVote={onVote} />
          ))}
        </div>
      ) : (
        <>
          <div className="flex flex-col gap-[var(--space-3)]">
            {poll.options.map((o) => (
              <ResultRow
                key={o.id}
                option={o}
                pct={total === 0 ? 0 : Math.round((o.count / total) * 100)}
                mine={poll.myChoice === o.id}
                secret={!!poll.secret}
              />
            ))}
          </div>

          {total === 0 && (
            <p className="text-[length:var(--text-sm)] text-[var(--text-muted)]">
              No votes yet.
            </p>
          )}

          <div className="flex items-center justify-between gap-[var(--space-3)]">
            {voted && poll.allowChange && !poll.closed && !readOnly ? (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onVote?.('')}
              >
                Change vote
              </Button>
            ) : (
              <span />
            )}
            {!poll.secret && total > 0 && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setShowVotes((s) => !s)}
              >
                {showVotes ? 'Hide votes' : 'See votes'}
              </Button>
            )}
          </div>

          {showVotes && !poll.secret && (
            <div className="flex flex-col gap-[var(--space-3)] border-t border-[var(--border-subtle)] pt-[var(--space-3)]">
              {poll.options
                .filter((o) => o.count > 0)
                .map((o) => (
                  <div key={o.id} className="flex flex-col gap-[var(--space-1)]">
                    <div className="text-[length:var(--text-xs)] font-medium text-[var(--text-primary)]">
                      {o.label} · {o.count}
                    </div>
                    <div className="flex flex-wrap gap-x-[var(--space-3)] gap-y-[var(--space-1)]">
                      {o.voters.map((v) => (
                        <span
                          key={v.id}
                          className="flex items-center gap-[var(--space-2)]"
                        >
                          <VoterFace voter={v} className="ring-0" />
                          <span className="text-[length:var(--text-sm)] text-[var(--text-primary)]">
                            {v.name}
                          </span>
                        </span>
                      ))}
                    </div>
                  </div>
                ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
