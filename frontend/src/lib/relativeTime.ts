import { parseSqliteTs } from './types'

const MINUTE = 60
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR
const WEEK = 7 * DAY
const MONTH = 30 * DAY
const YEAR = 365 * DAY

// Render a SQLite-native UTC timestamp (`YYYY-MM-DD HH:MM:SS`) as a short
// relative string. Always treats the wire value as UTC — see memory.md
// "Datetime on wire" pitfall — so a request that landed 'just now' doesn't
// drift by the viewer's timezone offset.
//
// Past tense only: the backend timestamps we feed in are last_seen_at /
// created_at, neither of which is in the future. If the diff comes out
// negative (clock skew), we surface 'just now' rather than '… from now'.
export function relativeTimeFromSqlite(s: string, now: Date = new Date()): string {
  const past = parseSqliteTs(s)
  const diffMs = now.getTime() - past.getTime()
  const seconds = Math.max(0, Math.floor(diffMs / 1000))

  if (seconds < 45) return 'just now'
  if (seconds < MINUTE * 2) return '1 minute ago'
  if (seconds < HOUR) return `${Math.floor(seconds / MINUTE)} minutes ago`
  if (seconds < HOUR * 2) return '1 hour ago'
  if (seconds < DAY) return `${Math.floor(seconds / HOUR)} hours ago`
  if (seconds < DAY * 2) return 'yesterday'
  if (seconds < WEEK) return `${Math.floor(seconds / DAY)} days ago`
  if (seconds < WEEK * 2) return '1 week ago'
  if (seconds < MONTH) return `${Math.floor(seconds / WEEK)} weeks ago`
  if (seconds < MONTH * 2) return '1 month ago'
  if (seconds < YEAR) return `${Math.floor(seconds / MONTH)} months ago`
  if (seconds < YEAR * 2) return '1 year ago'
  return `${Math.floor(seconds / YEAR)} years ago`
}

// Render a SQLite-native UTC timestamp as a compact local date (YYYY-MM-DD)
// for the 'Created' column. Uses `toLocaleDateString('en-CA')` to get a
// stable ISO-shape regardless of viewer locale (en-CA renders as
// `YYYY-MM-DD`, which we want here for terseness).
export function localDateFromSqlite(s: string): string {
  const d = parseSqliteTs(s)
  return d.toLocaleDateString('en-CA')
}
