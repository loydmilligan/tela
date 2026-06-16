import type { ComponentType } from 'react'
import {
  AlertTriangle,
  Bug,
  Eye,
  FilePlus,
  LogIn,
  LogOut,
  Pencil,
  Shield,
  Sparkles,
  Terminal,
  Activity,
} from 'lucide-react'
import type { EventEntry } from '../../lib/types'
import { relativeTimeFromSqlite } from '../../lib/relativeTime'
import { Badge } from '../ui/badge'
import { cn } from '../../lib/utils'

type Tone = 'muted' | 'danger' | 'accent'

interface Descriptor {
  Icon: ComponentType<{ width?: number; height?: number }>
  // Short group label for the trailing badge.
  group: string
  tone: Tone
  // The verb phrase shown after the actor (target rendered separately, bold).
  verb: string
}

// Map an event type to its icon + verb. access.* and unknown types fall through
// to a generic descriptor built from the type code.
function describe(type: string): Descriptor {
  switch (type) {
    case 'auth.login':
      return { Icon: LogIn, group: 'auth', tone: 'muted', verb: 'signed in' }
    case 'auth.logout':
      return { Icon: LogOut, group: 'auth', tone: 'muted', verb: 'signed out' }
    case 'auth.login_failed':
      return { Icon: AlertTriangle, group: 'auth', tone: 'danger', verb: 'failed to sign in' }
    case 'page.view':
      return { Icon: Eye, group: 'page', tone: 'muted', verb: 'viewed' }
    case 'page.create':
      return { Icon: FilePlus, group: 'page', tone: 'accent', verb: 'created' }
    case 'page.edit':
      return { Icon: Pencil, group: 'page', tone: 'muted', verb: 'edited' }
    case 'ask':
      return { Icon: Sparkles, group: 'ask', tone: 'accent', verb: 'asked' }
    case 'api.request':
      return { Icon: Terminal, group: 'api', tone: 'muted', verb: 'API request' }
    case 'client.error':
      return { Icon: Bug, group: 'error', tone: 'danger', verb: 'hit a client error' }
    default:
      if (type.startsWith('access.')) {
        // 'access.org_member.add' → 'org member add'
        const action = type.slice('access.'.length).replace(/[._]/g, ' ')
        return { Icon: Shield, group: 'access', tone: 'accent', verb: action }
      }
      return { Icon: Activity, group: type.split('.')[0] || 'event', tone: 'muted', verb: type }
  }
}

// Badge primitive ships muted/accent; the danger tone is carried by the red icon
// instead, so its badge stays muted.
const BADGE_VARIANT: Record<Tone, 'muted' | 'accent'> = {
  muted: 'muted',
  danger: 'muted',
  accent: 'accent',
}

export function EventRow({ event }: { event: EventEntry }) {
  const d = describe(event.type)
  const actor = event.actor_label || 'anonymous'
  // page.* events carry a title in target_label; render it set off from the verb.
  const showTarget = event.target_label !== ''
  return (
    <li
      className={cn(
        'm-0 list-none flex items-start gap-[var(--space-3)]',
        'px-[var(--space-3)] py-[var(--space-2)]',
        'rounded-[var(--radius-sm)]',
        'border border-[var(--border-subtle)] bg-[var(--surface-1)]',
      )}
    >
      <span
        className={cn(
          'mt-[2px] shrink-0',
          d.tone === 'danger' ? 'text-[var(--danger)]' : 'text-[var(--text-muted)]',
        )}
      >
        <d.Icon width={15} height={15} />
      </span>
      <div className="flex-1 min-w-0 flex flex-col gap-[1px]">
        <div className="flex items-baseline gap-[var(--space-2)] min-w-0 flex-wrap">
          <span className="text-[length:var(--text-sm)] text-[var(--text-primary)] font-[family-name:var(--font-sans)]">
            <span className="font-medium">{actor}</span> {d.verb}
            {showTarget ? (
              <>
                {' '}
                <span className="font-medium">{event.target_label}</span>
              </>
            ) : null}
          </span>
          {event.detail && d.group !== 'access' && d.group !== 'error' ? (
            <span className="truncate max-w-full text-[length:var(--text-xs)] text-[var(--text-muted)]">
              {event.detail}
            </span>
          ) : null}
        </div>
        {/* Client errors carry a multi-line message + stack; show it in full
            (the row truncation would hide the stack that makes it useful). */}
        {event.detail && d.group === 'error' ? (
          <pre className="m-0 mt-[var(--space-1)] max-h-[12rem] overflow-auto whitespace-pre-wrap break-words rounded-[var(--radius-sm)] bg-[var(--surface-2)] p-[var(--space-2)] text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-mono)]">
            {event.detail}
          </pre>
        ) : null}
        <span className="text-[length:var(--text-xs)] text-[var(--text-muted)] font-[family-name:var(--font-sans)]">
          {relativeTimeFromSqlite(event.created_at)}
          {event.ip ? ` · ${event.ip}` : ''}
        </span>
      </div>
      <Badge variant={BADGE_VARIANT[d.tone]}>{d.group}</Badge>
    </li>
  )
}
