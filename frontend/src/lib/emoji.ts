// Emoji dataset + lookup/search, lazy-loaded so the ~1,870-entry gemoji JSON
// lands in its own chunk (mirrors the mermaid / echarts dynamic-import idiom)
// rather than bloating the editor bundle. tela stores the actual Unicode emoji
// in the canonical markdown body — `:rocket:` is only an authoring convenience
// that the editor replaces with 🚀 — so there's no render/serialize path here,
// just the data the input rule (milkdown-emoji.ts) and the colon-autocomplete /
// `/` picker views read from.
//
// Names are GitHub's shortcode aliases (the `:rocket:` set), matching tela's
// GFM flavor. `ensureEmojiLoaded()` kicks off the import; `lookupEmoji` /
// `searchEmoji` / `emojiGroups` return empty until it resolves (callers re-run
// once the promise settles, or — for the synchronous input rule — simply no-op
// on the rare pre-load keystroke).

export interface EmojiEntry {
  emoji: string
  // Primary shortcode (first alias) — what we echo in the picker UI.
  name: string
  names: string[]
  tags: string[]
  category: string
}

let loadPromise: Promise<void> | null = null
let byName = new Map<string, string>()
let entries: EmojiEntry[] = []
let groups: { category: string; items: EmojiEntry[] }[] = []

// Picker column order — gemoji's category strings, arranged the way people
// expect an emoji keyboard to read.
const CATEGORY_ORDER = [
  'Smileys & Emotion',
  'People & Body',
  'Animals & Nature',
  'Food & Drink',
  'Activities',
  'Travel & Places',
  'Objects',
  'Symbols',
  'Flags',
]

export function ensureEmojiLoaded(): Promise<void> {
  if (!loadPromise) {
    loadPromise = import('gemoji').then(({ gemoji }) => {
      entries = gemoji.map((g) => ({
        emoji: g.emoji,
        name: g.names[0] ?? '',
        names: g.names,
        tags: g.tags,
        category: g.category,
      }))
      byName = new Map()
      for (const e of entries) {
        for (const n of e.names) if (!byName.has(n)) byName.set(n, e.emoji)
      }
      const buckets = new Map<string, EmojiEntry[]>()
      for (const e of entries) {
        const arr = buckets.get(e.category)
        if (arr) arr.push(e)
        else buckets.set(e.category, [e])
      }
      groups = CATEGORY_ORDER.filter((c) => buckets.has(c)).map((category) => ({
        category,
        items: buckets.get(category)!,
      }))
    })
  }
  return loadPromise
}

export function emojiReady(): boolean {
  return entries.length > 0
}

// Exact shortcode → emoji char. Returns undefined for unknown names (so the
// input rule leaves `:asdf:` as literal text) or before the dataset loads.
export function lookupEmoji(name: string): string | undefined {
  return byName.get(name)
}

export interface EmojiHit {
  emoji: string
  name: string
}

// Ranked prefix/substring search over shortcode names, then tags. Exact-name
// and name-prefix matches rank above tag matches; ties keep dataset order
// (gemoji is roughly frequency-sorted within a category). Cheap enough to run
// per keystroke against ~1,870 entries with no index.
export function searchEmoji(query: string, limit = 8): EmojiHit[] {
  const q = query.trim().toLowerCase()
  if (!q || !emojiReady()) return []
  const exact: EmojiHit[] = []
  const prefix: EmojiHit[] = []
  const sub: EmojiHit[] = []
  const tag: EmojiHit[] = []
  for (const e of entries) {
    const hit: EmojiHit = { emoji: e.emoji, name: e.name }
    let placed = false
    for (const n of e.names) {
      if (n === q) {
        exact.push(hit)
        placed = true
        break
      }
      if (n.startsWith(q)) {
        prefix.push(hit)
        placed = true
        break
      }
      if (n.includes(q)) {
        sub.push(hit)
        placed = true
        break
      }
    }
    if (!placed && e.tags.some((t) => t.includes(q))) tag.push(hit)
  }
  return [...exact, ...prefix, ...sub, ...tag].slice(0, limit)
}

// Category-grouped full list for the `/` picker grid (phase 2).
export function emojiGroups(): { category: string; items: EmojiEntry[] }[] {
  return groups
}
