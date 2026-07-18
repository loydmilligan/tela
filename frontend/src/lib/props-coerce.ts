// Coerce a text input to its natural JSON type for the page-properties editor
// (#15). This is the SAME discipline the set_prop schema fix enforces
// server-side: a stringified value is silently unqueryable — props containment,
// numeric compare, and sort all miss a number or bool stored as text. So the
// props editor must write `42` and `true`, never `"42"` / `"true"`.
export function coerceScalar(input: string): unknown {
  const t = input.trim()
  if (t === '') return ''
  if (t === 'true') return true
  if (t === 'false') return false
  if (t === 'null') return null
  if (/^-?\d+$/.test(t)) return parseInt(t, 10)
  if (/^-?\d*\.\d+$/.test(t)) return parseFloat(t)
  return input
}

// A scalar is inline-editable in the props popover; arrays/objects are shown
// read-only (editing JSON in a tiny popover is error-prone — a later surface).
export function isEditableScalar(v: unknown): boolean {
  return v === null || ['string', 'number', 'boolean'].includes(typeof v)
}
