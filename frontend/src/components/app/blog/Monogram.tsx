import { avatarStyle, monogram } from '../../../lib/blog'
import { cn } from '../../../lib/utils'

// Generated avatar tile — a deterministic tint + 1–2 letter monogram from a
// seed. The single source for the public surfaces' identity tiles (masthead,
// space cards), so there's no uploaded image to manage and every space/author
// has a stable look.
export function Monogram({
  name,
  seed,
  size = 'md',
  className,
}: {
  /** Text the monogram letters come from (e.g. a space or person's name). */
  name: string
  /** Tint seed; defaults to `name`. Pass a slug/handle for a more stable hue. */
  seed?: string
  size?: 'sm' | 'md'
  className?: string
}) {
  const sizing =
    size === 'sm'
      ? 'size-[2.5rem] rounded-[var(--radius-md)] text-[length:var(--text-base)]'
      : 'size-[3.5rem] rounded-[var(--radius-lg)] text-[length:var(--text-xl)]'
  return (
    <span
      aria-hidden
      className={cn(
        'grid shrink-0 place-items-center font-[family-name:var(--font-sans)] font-semibold leading-none select-none',
        sizing,
        className,
      )}
      style={avatarStyle(seed ?? name)}
    >
      {monogram(name)}
    </span>
  )
}
