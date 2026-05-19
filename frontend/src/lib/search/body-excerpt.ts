// Plain-text excerpt builder for tier-3 body hits + /search rows.
//
// Picks a window around the first case-insensitive match of `query` in `body`
// (defaulting to ~100 chars wide via `halfWidth = 50`). Newlines collapse to
// spaces so the excerpt reads as a single line. Never returns HTML — body-tier
// surfaces ship plain text in v0 (no `<mark>` highlighting). If the body has
// no direct substring match (e.g., Orama fuzzy hit), falls back to the head of
// the body so the row still carries useful context.

export function bodyExcerpt(
  body: string,
  query: string,
  halfWidth = 50,
): string {
  if (!body) return ''
  const flat = body.replace(/[\r\n]+/g, ' ')
  const width = halfWidth * 2
  if (!query) {
    return flat.length > width ? flat.slice(0, width) + '…' : flat
  }
  const pos = flat.toLowerCase().indexOf(query.toLowerCase())
  if (pos === -1) {
    return flat.length > width ? flat.slice(0, width) + '…' : flat
  }
  const start = Math.max(0, pos - halfWidth)
  const end = Math.min(flat.length, pos + query.length + halfWidth)
  let out = flat.slice(start, end)
  if (start > 0) out = '…' + out
  if (end < flat.length) out = out + '…'
  return out
}
