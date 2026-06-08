import type { Space } from './types'

// Who owns a space, for the "Owned by …" indicator.
//   - 'org'      → owned by an organization (`org` carries its name)
//   - 'personal' → the auto-provisioned personal home
//   - 'you'      → a regular space you own
export interface SpaceOwnership {
  kind: 'org' | 'personal' | 'you'
  org?: string
}

// Ownership is the explicit `owner_org` (spaces.org_id) when the backend sends
// it. Falls back to the principals' org entry for older/detail-shape spaces
// that lack the field — note that an org appearing in `principals` may be a
// share rather than the owner, so the explicit field is preferred and the
// fallback is best-effort (only trusted when exactly one org is present).
export function spaceOwnership(space: Space): SpaceOwnership {
  if (space.owner_org) return { kind: 'org', org: space.owner_org.name }
  if (space.is_personal) return { kind: 'personal' }
  const orgs = (space.principals ?? []).filter((p) => p.kind === 'org')
  if (orgs.length === 1) return { kind: 'org', org: orgs[0].name }
  return { kind: 'you' }
}
