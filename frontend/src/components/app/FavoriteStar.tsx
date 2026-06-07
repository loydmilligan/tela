import { Star } from 'lucide-react'
import { Button } from '../ui/button'
import { useFavoriteStatus, useToggleFavorite } from '../../lib/queries/favorites'

// Header star toggle — stars/unstars the current page for the signed-in user.
// A ghost Button (not a Radix Toggle) so the *icon* fills when active, rather
// than the whole button getting a pressed-state background. Mirrors FollowButton.
export function FavoriteStar({ pageId }: { pageId: number }) {
  const { data } = useFavoriteStatus(pageId)
  const toggle = useToggleFavorite()
  const isFavorited = data ?? false

  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      onClick={() => toggle.mutate({ pageId, isFavorited })}
      disabled={toggle.isPending}
      aria-label={isFavorited ? 'Remove from favorites' : 'Add to favorites'}
      title={isFavorited ? 'Remove from favorites' : 'Add to favorites'}
      className="h-[var(--space-8)] w-[var(--space-8)] p-0"
    >
      <Star
        width={16}
        height={16}
        className={isFavorited ? 'fill-[var(--accent)] text-[var(--accent)]' : undefined}
      />
    </Button>
  )
}
