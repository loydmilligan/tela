import { useNavigate } from '@tanstack/react-router'
import {
  ChevronUp,
  CreditCard,
  Globe,
  Link2,
  LogOut,
  MessageSquarePlus,
  Settings,
  Sparkles,
} from 'lucide-react'
import { useLogout, useMe } from '../../lib/queries/auth'
import { useTelaHomeHref } from '../../lib/queries/host-context'
import { useMyUsage } from '../../lib/queries/billing'
import { emitOpenFeedback } from '../../lib/feedbackEvent'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../ui/dropdown-menu'

// Sidebar-footer slot: opens a dropdown anchored on the current username with
// Settings (stub for M6.4) and Sign out. Bottom-anchored so the menu pops up
// rather than off the bottom edge.
export function UserMenu() {
  const telaHome = useTelaHomeHref()
  const me = useMe()
  const usage = useMyUsage()
  const logout = useLogout()
  const navigate = useNavigate()
  const user = me.data ?? null
  const planName = usage.data?.plan.name
  const feedbackUnseen = user?.feedback_unseen ?? 0
  // The only self-serve personal upgrade is Free → Plus (Plus is the top paid
  // personal tier; Unlimited is an internal comp tier). Show the prominent CTA
  // exactly then, so it converts the people it's for and stays out of the way
  // for everyone else. The billing tab still carries every tier + org upgrades.
  const canUpgrade = usage.data?.plan.key === 'personal_free'

  function goToBilling() {
    void navigate({ to: '/settings', search: { tab: 'billing' } })
  }

  async function handleSignOut() {
    try {
      await logout.mutateAsync()
    } catch {
      // Even if the network call fails the cache was reset onSettled, so the
      // user already looks signed out locally — just push them to /login.
    }
    void navigate({ to: '/login' })
  }

  if (!user) return null

  return (
    <div className="border-t border-[var(--border-subtle)] py-[var(--space-3)] px-[var(--space-3)] shrink-0 flex flex-col gap-[var(--space-2)]">
      {canUpgrade ? (
        <Button
          variant="primary"
          size="sm"
          className="w-full justify-center gap-[var(--space-2)]"
          onClick={goToBilling}
        >
          <Sparkles width={14} height={14} aria-hidden /> Upgrade to Plus
        </Button>
      ) : null}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            className="w-full justify-start gap-[var(--space-2)]"
            aria-label={`Account menu for ${user.username}`}
          >
            <span className="flex-1 text-left truncate">{user.username}</span>
            {feedbackUnseen > 0 ? (
              <span
                aria-label={`${feedbackUnseen} unread feedback`}
                className="size-[var(--space-2)] shrink-0 rounded-full bg-[var(--accent)]"
              />
            ) : null}
            {planName ? (
              <Badge variant="muted" className="shrink-0">
                {planName}
              </Badge>
            ) : null}
            <ChevronUp width={14} height={14} aria-hidden />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent side="top" align="start" className="min-w-[14rem]">
          <DropdownMenuItem
            onSelect={(e) => {
              e.preventDefault()
              void navigate({ to: '/shared' })
            }}
          >
            <Link2 width={14} height={14} /> Shared
          </DropdownMenuItem>
          <DropdownMenuItem
            onSelect={(e) => {
              e.preventDefault()
              // When there's unseen feedback, land directly on the Feedback tab
              // (where the badge lives) so the dot isn't a dead end at Profile.
              void navigate({
                to: '/settings',
                search: feedbackUnseen > 0 ? { tab: 'feedback' } : {},
              })
            }}
          >
            <Settings width={14} height={14} /> Settings
            {feedbackUnseen > 0 ? (
              <Badge variant="accent" className="ml-auto">
                {feedbackUnseen}
              </Badge>
            ) : null}
          </DropdownMenuItem>
          <DropdownMenuItem
            onSelect={(e) => {
              e.preventDefault()
              goToBilling()
            }}
          >
            <CreditCard width={14} height={14} /> Plan &amp; usage
            {planName ? (
              <Badge variant="muted" className="ml-auto">
                {planName}
              </Badge>
            ) : null}
          </DropdownMenuItem>
          <DropdownMenuItem
            onSelect={(e) => {
              e.preventDefault()
              // Close the menu, then open the shared feedback popover (anchored
              // on the header trigger) via the event bus.
              emitOpenFeedback()
            }}
          >
            <MessageSquarePlus width={14} height={14} /> Send feedback
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onSelect={(e) => {
              e.preventDefault()
              // Hard navigation, NOT router.navigate: "/" is owned by the
              // static landing (served by Caddy), not a TanStack route. The
              // ?home=1 hatch tells Caddy to serve the landing even though we
              // carry a session cookie — a client-side navigate would instead
              // resolve "/" in-app and bounce straight back to the space.
              // On an org custom domain the landing lives on the canonical
              // host, so prefix it (a relative "/" would stay on the org
              // domain).
              window.location.assign(
                (telaHome === '/' ? '' : telaHome) + '/?home=1',
              )
            }}
          >
            <Globe width={14} height={14} /> Landing page
          </DropdownMenuItem>
          <DropdownMenuItem
            onSelect={(e) => {
              e.preventDefault()
              void handleSignOut()
            }}
          >
            <LogOut width={14} height={14} /> Sign out
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
