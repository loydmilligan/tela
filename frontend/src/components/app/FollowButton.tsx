import { Bell } from 'lucide-react'
import { useSubscription, useToggleSubscription } from '../../lib/queries/subscriptions'
import { Button } from '../ui/button'

// Header follow toggle for a page — opts into "this page changed" notifications.
// Icon-only (like the favorite star) to keep the header compact; filled when
// following.
export function FollowButton({ pageId }: { pageId: number }) {
  const { data } = useSubscription('page', pageId)
  const toggle = useToggleSubscription('page', pageId)
  const following = data ?? false

  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      onClick={() => toggle.mutate(following)}
      disabled={toggle.isPending}
      aria-label={following ? 'Following this page — unfollow' : 'Follow this page'}
      title={
        following
          ? 'Following — you’ll be notified of changes'
          : 'Follow to be notified when this page changes'
      }
      className="h-[var(--space-8)] w-[var(--space-8)] p-0"
    >
      <Bell
        width={16}
        height={16}
        className={following ? 'fill-[var(--accent)] text-[var(--accent)]' : undefined}
      />
    </Button>
  )
}
