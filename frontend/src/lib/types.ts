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
  // Number of members with access. Present on the spaces list (sidebar shows a
  // count chip when > 1); optional elsewhere.
  member_count?: number
}

// Resolved public-link exposure of a page (backend exposure.go). Read-only,
// derived from active share links — pages have no stored visibility. "private"
// = space members only (the resting state); "public"/"password" = reachable by
// an open / password-protected link. `inherited` = exposure comes only from an
// ancestor's include-descendants share. See docs/visibility-model.md.
export type ExposureState = 'private' | 'public' | 'password'

export interface PageExposure {
  state: ExposureState
  inherited: boolean
  expires_at: string | null
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
  // Present on tree (`?tree=1`) and flat list rows, and attached by `usePage`
  // from the GET /api/pages/{id} sibling field. Optional so older cached rows
  // and optimistic nodes stay valid.
  exposure?: PageExposure | null
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

// Row in GET /api/users/me/sessions (M6.2). Timestamps are SQLite-native
// `YYYY-MM-DD HH:MM:SS` strings (no `Z`); parse via `parseSqliteTs` and
// related helpers — never `new Date(s)` directly. `current=true` flags the
// session whose cookie made the request, which the UI uses to mark the row
// and disable its revoke control.
export interface SessionRow {
  id: string
  last_seen_at: string
  expires_at: string
  created_at: string
  user_agent: string
  current: boolean
}

// Row in GET /api/spaces/{id}/members (M6.2). Mirrors backend's
// spaceMemberDTO. Backend orders rows owner → editor → viewer (role ASC
// sorts that way alphabetically) then username ASC.
export interface SpaceMember {
  user_id: number
  username: string
  role: 'owner' | 'editor' | 'viewer'
  created_at: string
  updated_at: string
}

// Row in GET /api/admin/users (M6.2). Mirrors backend's adminUserDTO.
// Timestamps are SQLite-native — render via localDateFromSqlite for the
// 'Created' column.
export interface AdminUserRow {
  id: number
  username: string
  email: string | null
  email_verified: boolean
  is_instance_admin: boolean
  is_active: boolean
  created_at: string
  updated_at: string
}

// Organizations (#153). An org is a grantable principal: a space can be shared
// with a whole org via space_grants, and every member gains the granted role
// through the space_access view. Mirrors backend's orgDTO.
export type OrgRole = 'admin' | 'member'

export interface Org {
  id: number
  name: string
  slug: string
  member_count: number
  // The caller's own org_role, or null when they only see the org as an
  // instance-admin (no membership row).
  my_role: OrgRole | null
  created_at: string
  updated_at: string
}

// Row in GET /api/orgs/{id}/members. Mirrors backend's orgMemberDTO.
export interface OrgMember {
  user_id: number
  username: string
  email: string | null
  org_role: OrgRole
  created_at: string
  updated_at: string
}

// Row in GET /api/spaces/{id}/grants — an org's access to a space. Keyed by
// grant id (principal kind is an implementation detail). Org grants are limited
// to editor/viewer. Mirrors backend's spaceGrantDTO.
export interface SpaceGrant {
  id: number
  org_id: number
  org_name: string
  org_slug: string
  role: 'editor' | 'viewer'
  created_at: string
  updated_at: string
}

// Row in GET /api/admin/org-domains — an auto-join email-domain mapping.
// Mirrors backend's orgDomainDTO.
export interface OrgDomain {
  domain: string
  org_id: number
  org_name: string
  org_role: OrgRole
  created_at: string
}

// Three-rung scope ceiling on a personal access token. See
// backend/internal/auth/api_key.go — `admin` implies write+read, `write`
// implies read.
export type ApiKeyScope = 'read' | 'write' | 'admin'

// Row in GET /api/api_keys (M16.A.1). Mirrors backend's apiKeyDTO. `key` is
// populated ONLY on the POST create response — list/get omit it. `space_id`
// null means "all spaces"; otherwise the key is scoped to that single space.
export interface ApiKeyRow {
  id: number
  name: string
  key_prefix: string
  scope: ApiKeyScope
  space_id: number | null
  last_used_at: string | null
  expires_at: string | null
  created_at: string
  revoked_at: string | null
  key?: string
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
