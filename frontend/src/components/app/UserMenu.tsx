import { useNavigate } from '@tanstack/react-router'
import { ChevronUp, Globe, Link2, LogOut, Settings } from 'lucide-react'
import { useLogout, useMe } from '../../lib/queries/auth'
import { useTelaHomeHref } from '../../lib/queries/host-context'
import { useMyUsage } from '../../lib/queries/billing'
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
    <div className="border-t border-[var(--border-subtle)] py-[var(--space-3)] px-[var(--space-3)] shrink-0">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            className="w-full justify-start gap-[var(--space-2)]"
            aria-label={`Account menu for ${user.username}`}
          >
            <span className="flex-1 text-left truncate">{user.username}</span>
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
              void navigate({ to: '/settings' })
            }}
          >
            <Settings width={14} height={14} /> Settings
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
