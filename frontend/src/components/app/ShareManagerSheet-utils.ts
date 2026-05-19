import { ApiError } from '../../lib/api'
import { parseSqliteTs } from '../../lib/types'

// Friendly copy per known {error, code} the management endpoints can emit.
// Falls through to the raw API message for anything we don't recognise so the
// developer still sees something useful.
export function shareErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'viewer_no_write':
        return 'Editor permission required.'
      case 'not_found':
        return 'Page or share no longer exists.'
      case 'unauthorized':
        return 'Your session expired — please sign in.'
      case 'conflict':
        return 'This share was revoked. Refresh and try again.'
      case 'bad_request': {
        if (/expires_at/i.test(err.message)) return 'Expiry must be in the future.'
        return "That doesn't look right."
      }
      default:
        return err.message || 'Something went wrong.'
    }
  }
  if (err instanceof Error) return err.message
  return 'Something went wrong.'
}

// Convert a datetime-local input value ("2026-05-21T14:30") to the SQLite wire
// format ("2026-05-21 14:30:00"). Empty input → empty string. The backend
// rejects an ISO string with `T` so callers must normalise before POSTing.
export function datetimeLocalToSqlite(v: string): string {
  if (!v) return ''
  const trimmed = v.trim()
  if (!trimmed) return ''
  const withSeconds = /:\d{2}$/.test(trimmed.slice(10))
    ? trimmed
    : trimmed + ':00'
  return withSeconds.replace('T', ' ')
}

// Accept "YYYY-MM-DD HH:MM:SS" as-is; normalise "YYYY-MM-DDTHH:MM" to the
// wire format. Empty trimmed → empty string.
export function normaliseExpiryInput(raw: string): string {
  const trimmed = raw.trim()
  if (!trimmed) return ''
  return trimmed.includes('T') ? datetimeLocalToSqlite(trimmed) : trimmed
}

// Render the expiry as either "Expires in Nd / Nh / Nm" or "Expired Nh ago".
// Returns null when expires_at is null (caller decides whether to render the
// badge at all).
export function formatExpiry(
  expiresAt: string | null,
  now: number,
): string | null {
  if (!expiresAt) return null
  const target = parseSqliteTs(expiresAt).getTime()
  const diffMs = target - now
  const expired = diffMs <= 0
  const absMs = Math.abs(diffMs)
  const minutes = Math.floor(absMs / 60_000)
  const hours = Math.floor(minutes / 60)
  const days = Math.floor(hours / 24)
  let unit: string
  if (days >= 1) unit = `${days}d`
  else if (hours >= 1) unit = `${hours}h`
  else unit = `${Math.max(1, minutes)}m`
  return expired ? `Expired ${unit} ago` : `Expires in ${unit}`
}
