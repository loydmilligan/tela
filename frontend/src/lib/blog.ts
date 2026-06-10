// Display helpers for the public blog surfaces (space front page, /u/{handle}).
// Deterministic, content-derived visuals so a space/author/post has a stable
// identity even with no uploaded avatar or cover image.

// Stable 0..359 hue from a string — same input always yields the same hue, so a
// space/author keeps its colour across renders. Small FNV-ish rolling hash.
export function hueFromString(s: string): number {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return Math.abs(h) % 360
}

// One/two-letter monogram for an avatar tile. Initials of the first two words,
// else the first two letters; uppercased.
export function monogram(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean)
  if (words.length === 0) return '·'
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase()
  return (words[0][0] + words[1][0]).toUpperCase()
}

// Tinted avatar surface + readable foreground, in OKLCH so it sits naturally
// over any theme. Mid lightness / modest chroma keeps it legible light or dark.
export function avatarStyle(seed: string): { background: string; color: string } {
  const hue = hueFromString(seed)
  return {
    background: `oklch(0.62 0.16 ${hue})`,
    color: `oklch(0.99 0.02 ${hue})`,
  }
}

// --- Shared derivations + styles ------------------------------------------
// One source for the small bits of logic/markup the public surfaces repeated:
// post ordering, the page→tree fold, and the filter-chip classes.

// Newest-first by published date (UTC strings sort lexicographically), id as a
// stable tiebreak. Used by the space index, author home, and the reader's
// prev/next post nav so "latest" means the same thing everywhere.
export function sortByNewest<T extends { created_at: string; id: number }>(
  pages: T[],
): T[] {
  return [...pages].sort(
    (a, b) => b.created_at.localeCompare(a.created_at) || b.id - a.id,
  )
}

// The space's "posts" — its top-level pages (nested pages are sub-sections).
export function topLevelPosts<T extends { parent_id: number | null }>(
  pages: T[],
): T[] {
  return pages.filter((p) => p.parent_id == null)
}

export type TreeNode<T> = T & { children: TreeNode<T>[] }

// Fold a flat page list into a tree by parent_id, siblings in author position
// order (id tiebreak). A child whose parent isn't in the set surfaces as a root
// rather than vanishing. Shared by the public reader's space-nav.
export function buildPageTree<
  T extends { id: number; parent_id: number | null; position: number },
>(pages: T[]): TreeNode<T>[] {
  const byId = new Map<number, TreeNode<T>>()
  for (const p of pages) byId.set(p.id, { ...p, children: [] } as TreeNode<T>)
  const roots: TreeNode<T>[] = []
  for (const node of byId.values()) {
    const parent = node.parent_id != null ? byId.get(node.parent_id) : undefined
    if (parent) parent.children.push(node)
    else roots.push(node)
  }
  const sortRec = (nodes: TreeNode<T>[]) => {
    nodes.sort((a, b) => a.position - b.position || a.id - b.id)
    for (const n of nodes) sortRec(n.children)
  }
  sortRec(roots)
  return roots
}

// Filter-chip classes (tag bar on the index, sort toggle on /discover). One
// definition so the two stay visually identical.
const CHIP_BASE =
  'rounded-[var(--radius-sm)] border px-[var(--space-3)] py-[2px] text-[length:var(--text-xs)] no-underline transition-colors duration-[var(--duration-fast)]'
const CHIP_ON = 'border-[var(--accent)] bg-[var(--accent)] text-[var(--text-inverse)]'
const CHIP_OFF =
  'border-[var(--border-subtle)] text-[var(--text-muted)] hover:border-[var(--border-strong)] hover:text-[var(--text-primary)]'
export function blogChip(active: boolean): string {
  return `${CHIP_BASE} ${active ? CHIP_ON : CHIP_OFF}`
}

// The generated cover background for a post with no `cover` image: a title-
// seeded diagonal gradient under a fine "engineer's-notebook" graph grid. Paired
// with a large faded monogram in PostCard, this reads as a deliberate cover, not
// a blank placeholder. Deterministic (same title → same cover) and theme-
// independent (it's a fixed decorative surface, like the avatar tint).
export function coverBackground(seed: string): string {
  const hue = hueFromString(seed)
  const hue2 = (hue + 38) % 360
  return [
    'linear-gradient(rgba(255,255,255,0.09) 1px, transparent 1px) 0 0 / 22px 22px',
    'linear-gradient(90deg, rgba(255,255,255,0.09) 1px, transparent 1px) 0 0 / 22px 22px',
    `linear-gradient(135deg, oklch(0.7 0.13 ${hue}), oklch(0.58 0.14 ${hue2}))`,
  ].join(', ')
}
