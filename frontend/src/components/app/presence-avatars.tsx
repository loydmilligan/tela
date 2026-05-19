import { useEffect, useState } from 'react'
import type { Awareness } from 'y-protocols/awareness'
import { useActivePeers, type AwarenessPeer } from '../../lib/collab/use-awareness'
import { Avatar, type AvatarTone } from '../ui/avatar'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '../ui/tooltip'
import { cn } from '../../lib/utils'

const MAX_VISIBLE = 5
const TRANSITION_MS = 120

export interface PresenceAvatarsProps {
  // `null` → not in a collab session (viewer fallback or non-collab mount).
  // The component renders nothing in that state so it's safe to slot into
  // PageView's header unconditionally.
  awareness: Awareness | null
  className?: string
}

// PresenceAvatars renders the live peer list as an overlapping avatar pill
// stack. The hook does dedupe by user.id, so two tabs of the same human
// surface as one avatar. We include self in the stack (helps "I'm here"
// affordance and lays the groundwork for #66 cursor labels).
export function PresenceAvatars({ awareness, className }: PresenceAvatarsProps) {
  const peers = useActivePeers(awareness)
  const mounted = useMountedPeers(peers)

  if (awareness == null) return null
  if (mounted.length === 0) return null

  const visible = mounted.slice(0, MAX_VISIBLE)
  const overflow = mounted.length - visible.length

  return (
    <div
      className={cn('flex items-center', className)}
      aria-label={`${mounted.length} ${mounted.length === 1 ? 'person' : 'people'} viewing this page`}
    >
      {visible.map((entry) => (
        <PresencePill key={entry.peer.user.id} entry={entry} />
      ))}
      {overflow > 0 ? (
        <Avatar
          size="sm"
          tone="neutral"
          className="-ml-[var(--space-2)] ring-2 ring-[var(--surface-1)]"
          aria-label={`${overflow} more`}
        >
          {`+${overflow}`}
        </Avatar>
      ) : null}
    </div>
  )
}

interface PillEntry {
  peer: AwarenessPeer
  // Mount phase tracks the fade transition. 'entering' becomes 'present' on
  // the next frame so the opacity transition applies; 'leaving' rides out
  // before the entry is removed from the DOM.
  phase: 'entering' | 'present' | 'leaving'
}

function PresencePill({ entry }: { entry: PillEntry }) {
  const { peer, phase } = entry
  const tone = toneForColorIdx(peer.user.colorIdx)
  const initial = initialFor(peer.user.username)
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Avatar
          size="sm"
          tone={tone}
          className={cn(
            '-ml-[var(--space-2)] first:ml-0 ring-2 ring-[var(--surface-1)]',
            'transition-opacity duration-[var(--duration-fast)] ease-[var(--ease-out)]',
            phase === 'present' ? 'opacity-100' : 'opacity-0',
          )}
        >
          {initial}
        </Avatar>
      </TooltipTrigger>
      <TooltipContent>{peer.user.username}</TooltipContent>
    </Tooltip>
  )
}

// Initials policy: take the first character of the username and uppercase it.
// Usernames are case-sensitive in v0 (memory: "Initials extraction: don't
// sometimes uppercase, sometimes not") — pick a deterministic rule.
function initialFor(username: string): string {
  const trimmed = username.trim()
  if (trimmed.length === 0) return '?'
  return trimmed.charAt(0).toUpperCase()
}

function toneForColorIdx(idx: number): AvatarTone {
  // colorIdx is 0..7; map to 1..8 collab tones. Defensive clamp protects
  // against future schema drift sending out-of-range values.
  const safe = ((idx % 8) + 8) % 8
  return (`collab-${safe + 1}` as AvatarTone)
}

// useMountedPeers wraps the dedup'd peer list with enter/leave phases so the
// avatar fade-in/out is smooth. Peers landing for the first time are
// 'entering' for one frame then 'present'; a peer that drops out goes to
// 'leaving' and is removed after the transition.
function useMountedPeers(peers: AwarenessPeer[]): PillEntry[] {
  const [entries, setEntries] = useState<PillEntry[]>([])

  useEffect(() => {
    setEntries((prev) => {
      const next: PillEntry[] = []
      const byUserId = new Map(prev.map((e) => [e.peer.user.id, e]))
      const seenIds = new Set<number>()
      for (const peer of peers) {
        seenIds.add(peer.user.id)
        const existing = byUserId.get(peer.user.id)
        if (existing) {
          // Keep latest peer payload (username might have changed) and resume
          // present phase if it was mid-leave.
          next.push({ peer, phase: 'present' })
        } else {
          next.push({ peer, phase: 'entering' })
        }
      }
      for (const existing of prev) {
        if (seenIds.has(existing.peer.user.id)) continue
        if (existing.phase !== 'leaving') {
          next.push({ ...existing, phase: 'leaving' })
        }
      }
      return next
    })
  }, [peers])

  // Promote 'entering' → 'present' on the next frame so the opacity
  // transition actually animates.
  useEffect(() => {
    if (entries.every((e) => e.phase !== 'entering')) return
    const raf = window.requestAnimationFrame(() => {
      setEntries((prev) =>
        prev.map((e) =>
          e.phase === 'entering' ? { ...e, phase: 'present' } : e,
        ),
      )
    })
    return () => window.cancelAnimationFrame(raf)
  }, [entries])

  // Drop 'leaving' entries after the fade.
  useEffect(() => {
    if (entries.every((e) => e.phase !== 'leaving')) return
    const t = window.setTimeout(() => {
      setEntries((prev) => prev.filter((e) => e.phase !== 'leaving'))
    }, TRANSITION_MS)
    return () => window.clearTimeout(t)
  }, [entries])

  return entries
}
