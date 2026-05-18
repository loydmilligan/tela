// Wire types — mirror the Go JSON tags exactly (snake_case).
// Timestamps come from SQLite as `YYYY-MM-DD HH:MM:SS` (no `Z`, no offset). Keep them
// as strings on the wire and parse with `parseSqliteTs` so they aren't interpreted as
// local time. See memory.md known pitfall.

export interface Space {
  id: number
  name: string
  slug: string
  created_at: string
  updated_at: string
}

export interface Page {
  id: number
  space_id: number
  parent_id: number | null
  title: string
  body: string
  position: number
  created_at: string
  updated_at: string
}

export interface PageTreeNode extends Page {
  children: PageTreeNode[]
}

// Flat cross-space row returned by `/api/pages/all` (M5.2b). Powers the
// `[[Page]]` autocomplete picker — breadcrumb is root → immediate parent
// (page title excluded). `space_name` lets the picker disambiguate rows
// whose visible breadcrumb collides across spaces.
export interface PageListItem {
  id: number
  space_id: number
  space_name: string
  title: string
  breadcrumb: string[]
}

// Row in /api/pages/{id}/backlinks (M5.2b). `snippet` wraps a nearby word
// in literal `<mark>…</mark>` from a ±60-byte window around the first
// `tela://page/{id}` URL (URL stripped). Same raw-HTML contract as
// `/api/search` — render via `HighlightedSnippet`, never
// `dangerouslySetInnerHTML`. Empty snippet = bare-URL source body; the
// renderer should hide the snippet line in that case.
export interface Backlink {
  page_id: number
  space_id: number
  space_name: string
  title: string
  breadcrumb: string[]
  snippet: string
}

export interface CreateSpaceInput {
  name: string
  slug?: string
}

export interface UpdateSpaceInput {
  name?: string
  slug?: string
}

export interface CreatePageInput {
  space_id: number
  parent_id?: number | null
  title: string
  body?: string
}

// PATCH /api/pages/{id} only accepts title and body — space_id and parent_id are
// move-only via /move. Typed accordingly to keep callers honest.
export interface UpdatePageInput {
  title?: string
  body?: string
}

// `parent_id`: omit to keep current; pass explicit `null` to make root.
// `space_id`: omit to keep current; pass to move across spaces (descendants follow).
export interface MovePageInput {
  space_id?: number
  parent_id?: number | null
  position?: number
}

export interface ApiErrorBody {
  error: string
  code: string
}

export function parseSqliteTs(s: string): Date {
  // SQLite emits "YYYY-MM-DD HH:MM:SS" UTC with no zone marker. Replace the space with
  // 'T' and append 'Z' so `new Date()` parses as UTC rather than local time.
  const iso = s.includes('T') ? s : s.replace(' ', 'T')
  const withZone = /[zZ]|[+-]\d{2}:?\d{2}$/.test(iso) ? iso : iso + 'Z'
  return new Date(withZone)
}
