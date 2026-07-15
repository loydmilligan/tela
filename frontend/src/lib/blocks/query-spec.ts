// Milkdown-free query render core. SINGLE SOURCE shared by the editor's query
// decoration (milkdown-query.ts) and the read-only view renderer's live table
// (QueryBlockView via MarkdownView). A ` ```query ` fenced block carries a small
// YAML-ish spec; the read view POSTs it to /api/pages/query and renders the
// matching pages as a table (a Dataview analog). See docs/page-properties.md.
//
// The spec is parsed synchronously here (a tiny YAML subset — scalars, one inline
// or block `where` mapping, an inline `columns` list) so both the editor preview
// and the React table render without an async parse.
//
// Spec shape (v1):
//   where: { type: incident, status: active }   # props @> containment
//   space: here            # 'here' | <space id> | omit = all readable spaces
//   columns: [title, status, updated]
//   sort: -updated         # (-)updated | (-)created | (-)title
//   limit: 25

export interface QuerySpec {
  /** props @> containment filter. Empty → all readable pages. */
  where: Record<string, unknown>
  /** 'here' (the block's own space) | a space id | undefined = all readable. */
  space?: 'here' | number
  /** Columns to render. Special: title, created, updated, space; else a prop key. */
  columns: string[]
  /** Whitelisted sort key (validated server-side). */
  sort?: string
  /** Row cap (clamped server-side). */
  limit?: number
}

export interface QuerySpecError {
  error: string
}

export function isQueryError(
  spec: QuerySpec | QuerySpecError,
): spec is QuerySpecError {
  return 'error' in spec
}

function unquote(s: string): string {
  const t = s.trim()
  if (t.length >= 2) {
    const q = t[0]
    if ((q === '"' || q === "'") && t[t.length - 1] === q) return t.slice(1, -1)
  }
  return t
}

// Coerce a scalar to bool / number / string, matching how the backend's
// props @> containment compares JSON values (a numeric or bool prop must be
// queried as a number/bool, not a string).
function scalar(s: string): unknown {
  const t = unquote(s)
  if (t === 'true') return true
  if (t === 'false') return false
  if (t === 'null') return null
  if (/^-?\d+$/.test(t)) return parseInt(t, 10)
  if (/^-?\d*\.\d+$/.test(t)) return parseFloat(t)
  return t
}

// Split a flow list/map body on top-level commas (no nesting in v1).
function splitItems(s: string): string[] {
  return s
    .split(',')
    .map((x) => x.trim())
    .filter((x) => x.length > 0)
}

function parseInlineList(s: string): string[] {
  let t = s.trim()
  if (t.startsWith('[') && t.endsWith(']')) t = t.slice(1, -1)
  return splitItems(t).map(unquote)
}

// Parse `key: value` pairs into `out`. Used for both an inline `{…}` flow map and
// a block-indented mapping.
function assignPairs(pairs: string[], out: Record<string, unknown>) {
  for (const pair of pairs) {
    const idx = pair.indexOf(':')
    if (idx < 0) continue
    const k = unquote(pair.slice(0, idx))
    if (k === '') continue
    out[k] = scalar(pair.slice(idx + 1))
  }
}

export function parseQuerySpec(code: string): QuerySpec | QuerySpecError {
  const lines = code.split('\n')
  const where: Record<string, unknown> = {}
  let space: 'here' | number | undefined
  let columns: string[] = []
  let sort: string | undefined
  let limit: number | undefined

  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i]
    if (/^\s/.test(rawLine)) continue // indented lines are consumed with their key
    const line = rawLine.trim()
    if (line === '' || line.startsWith('#')) continue
    const idx = line.indexOf(':')
    if (idx < 0) continue
    const key = line.slice(0, idx).trim().toLowerCase()
    const val = line.slice(idx + 1).trim()

    switch (key) {
      case 'where': {
        if (val.startsWith('{')) {
          const body = val.replace(/^\{/, '').replace(/\}$/, '')
          assignPairs(splitItems(body), where)
        } else if (val === '') {
          // Block mapping: consume following indented `  k: v` lines.
          const block: string[] = []
          while (i + 1 < lines.length && /^\s+\S/.test(lines[i + 1])) {
            block.push(lines[i + 1].trim())
            i++
          }
          assignPairs(block, where)
        } else {
          return { error: 'where must be a mapping' }
        }
        break
      }
      case 'space': {
        const sv = unquote(val).toLowerCase()
        if (sv === '' || sv === 'all') space = undefined
        else if (sv === 'here') space = 'here'
        else if (/^\d+$/.test(sv)) space = parseInt(sv, 10)
        else return { error: `invalid space "${val}"` }
        break
      }
      case 'columns':
        columns = parseInlineList(val)
        break
      case 'sort':
        sort = unquote(val)
        break
      case 'limit': {
        const n = parseInt(unquote(val), 10)
        if (!Number.isFinite(n)) return { error: 'limit must be a number' }
        limit = n
        break
      }
      default:
        break
    }
  }

  if (columns.length === 0) columns = ['title', 'updated']
  return { where, space, columns, sort, limit }
}

// A compact one-line description of the query for the editor preview.
function summarize(spec: QuerySpec): string {
  const parts: string[] = []
  const keys = Object.keys(spec.where)
  parts.push(
    keys.length
      ? keys.map((k) => `${k}=${String(spec.where[k])}`).join(', ')
      : 'all pages',
  )
  if (spec.space === 'here') parts.push('this space')
  else if (typeof spec.space === 'number') parts.push(`space ${spec.space}`)
  if (spec.sort) parts.push(`sort ${spec.sort}`)
  if (spec.limit) parts.push(`limit ${spec.limit}`)
  return parts.join(' · ')
}

// buildQueryPreview renders the editor-side decoration: a static, non-editable
// summary of what the block will query. The live table is a read-view concern
// (QueryBlockView) — the same stance the chart/field blocks take.
export function buildQueryPreview(code: string): HTMLElement {
  const dom = document.createElement('div')
  dom.className = 'tela-query-preview'
  dom.setAttribute('contenteditable', 'false')

  const spec = parseQuerySpec(code)
  if (isQueryError(spec)) {
    dom.classList.add('tela-query-error')
    dom.textContent = spec.error
    return dom
  }

  const label = document.createElement('span')
  label.className = 'tela-query-preview-label'
  label.textContent = 'Query'

  const summary = document.createElement('span')
  summary.className = 'tela-query-preview-summary'
  summary.textContent = summarize(spec)

  dom.append(label, summary)
  return dom
}
