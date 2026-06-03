// Cosmetic page slugs — the Confluence-style URL piece (docs/visibility-model.md).
//
// MUST stay in parity with the backend (backend/internal/api/slug.go): same
// transliteration map, same truncation. The slug is never canonical (the page
// id / share token is), but matching the backend keeps the address bar from
// flickering between a backend-emitted slug (og:url, share links) and the
// FE-canonicalised one.

const TRANSLIT: Record<string, string> = {
  ç: 'c', Ç: 'c', ğ: 'g', Ğ: 'g', ı: 'i', İ: 'i',
  ö: 'o', Ö: 'o', ş: 's', Ş: 's', ü: 'u', Ü: 'u',
  à: 'a', á: 'a', â: 'a', ä: 'a', ã: 'a', å: 'a',
  è: 'e', é: 'e', ê: 'e', ë: 'e',
  ì: 'i', í: 'i', î: 'i', ï: 'i',
  ò: 'o', ó: 'o', ô: 'o', õ: 'o',
  ù: 'u', ú: 'u', û: 'u',
  ñ: 'n', Ñ: 'n', ß: 'ss', æ: 'ae', œ: 'oe',
}

const MAX_SLUG_LEN = 60

// pageSlug derives a URL-safe, lowercase, hyphen-joined slug from a title,
// truncated at a word boundary. Returns '' when nothing usable remains (e.g. an
// emoji- or CJK-only title) — callers then use the bare /p/{id} form.
export function pageSlug(title: string): string {
  let out = ''
  // for...of iterates by code point, so emoji don't split.
  for (const ch of title) {
    out += ch in TRANSLIT ? TRANSLIT[ch] : ch.toLowerCase()
  }
  let s = out.replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
  if (s.length > MAX_SLUG_LEN) {
    s = s.slice(0, MAX_SLUG_LEN)
    const i = s.lastIndexOf('-')
    if (i > 0) s = s.slice(0, i)
    s = s.replace(/^-+|-+$/g, '')
  }
  return s
}

// pagePath builds the in-app route path for a page, with the cosmetic slug
// appended when the title yields one.
export function pagePath(spaceId: number, pageId: number, title: string): string {
  const base = `/spaces/${spaceId}/pages/${pageId}`
  const s = pageSlug(title)
  return s ? `${base}/${s}` : base
}
