// Pure, Milkdown-free embed-provider resolution. SINGLE SOURCE shared by the
// editor's embed schema (milkdown-embed.ts) and the view renderer. Returns a
// provider iframe src for a watch/share URL, or null when the provider isn't on
// the iframe allowlist (caller falls back to a link card). https-only.
// See docs/view-edit-split.md.
export function embedIframeSrc(raw: string): string | null {
  let u: URL
  try {
    u = new URL(raw.trim())
  } catch {
    return null
  }
  if (u.protocol !== 'https:') return null
  const host = u.hostname.replace(/^www\./, '')

  // YouTube — watch?v=, youtu.be/<id>, /embed/<id>, /shorts/<id>.
  if (host === 'youtube.com' || host === 'm.youtube.com') {
    const v = u.searchParams.get('v')
    if (v && /^[\w-]{11}$/.test(v)) return `https://www.youtube.com/embed/${v}`
    const m = u.pathname.match(/^\/(?:embed|shorts)\/([\w-]{11})/)
    if (m) return `https://www.youtube.com/embed/${m[1]}`
  }
  if (host === 'youtu.be') {
    const m = u.pathname.match(/^\/([\w-]{11})/)
    if (m) return `https://www.youtube.com/embed/${m[1]}`
  }
  // Vimeo — vimeo.com/<numeric id>.
  if (host === 'vimeo.com') {
    const m = u.pathname.match(/^\/(\d+)/)
    if (m) return `https://player.vimeo.com/video/${m[1]}`
  }
  // Loom — loom.com/share/<hash>.
  if (host === 'loom.com') {
    const m = u.pathname.match(/^\/(?:share|embed)\/([\w-]+)/)
    if (m) return `https://www.loom.com/embed/${m[1]}`
  }
  return null
}
