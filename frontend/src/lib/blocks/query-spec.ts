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
//   target: pages          # 'pages' (default) | 'comments'
//   where: { type: incident, status: active }   # props @> containment
//   space: here            # 'here' | <space id> | omit = all readable spaces
//   columns: [title, status, updated]
//   sort: -updated         # (-)updated | (-)created | (-)title
//   limit: 25
//
// target: comments filters COMMENT props instead — the change/decision log:
//   target: comments
//   where: { type: change }
//   columns: [change_summary, author, created]
//   sort: -created         # comments sort by (-)created | (-)updated only

/**
 * What the block queries. `pages` (default) filters a page's OWN props;
 * `comments` filters the props on timestamped events ABOUT pages — the
 * change/decision log. One authoring surface, two typed backends: the block
 * routes to /api/pages/query or /api/comments/query, which return different row
 * shapes (a comment carries body/author/page context).
 */
export type QueryTarget = 'pages' | 'comments'

/** One operator condition (query v2), e.g. cost > 100. ANDed with containment. */
export type QueryOp = 'gt' | 'lt' | 'gte' | 'lte' | 'ne' | 'contains' | 'exists'
export interface QueryFilter {
  key: string
  op: QueryOp
  value?: unknown
}

/** One sort term (query v2): a field (special column or prop key) + direction. */
export interface QuerySort {
  field: string
  dir: 'asc' | 'desc'
}

/** One aggregation function (query v2). */
export interface AggFn {
  fn: 'sum' | 'count' | 'avg' | 'min' | 'max'
  key?: string
  as: string
}
export interface AggregateSpec {
  fns: AggFn[]
  group_by?: string
}

/**
 * A display-only computed column (query v2): `<prop> <op> <number> as <alias>`.
 * Evaluated CLIENT-SIDE at render from the row's props — never sent to SQL, so
 * it adds no injection surface. Cannot be sorted or aggregated in v1.
 */
export interface ComputedColumn {
  alias: string
  prop: string
  op: '+' | '-' | '*' | '/'
  literal: number
}

export interface QuerySpec {
  /** 'pages' (default) | 'comments'. */
  target: QueryTarget
  /** props @> containment filter. Empty → all readable pages. */
  where: Record<string, unknown>
  /** Operator conditions (v2), ANDed with `where`. */
  filters: QueryFilter[]
  /** 'here' (the block's own space) | a space id | undefined = all readable. */
  space?: 'here' | number
  /**
   * comments target only: scope to ONE page's comments — 'here' is the block's
   * own page (the changelog-footer case). Distinct from `space`, and distinct
   * from the page CONTEXT the block sends to resolve 'here'.
   */
  page?: 'here' | number
  /** Columns to render. Special: title, created, updated, space; else a prop key or a computed alias. */
  columns: string[]
  /** Display-only computed columns (v2), keyed into `columns` by alias. */
  computed: ComputedColumn[]
  /** v1 whitelisted single sort key (kept for the preview; server accepts it). */
  sort?: string
  /** v2 multi-key sort (sort by any prop). */
  order?: QuerySort[]
  /** Aggregation rollup (v2). When set, the block renders group rows, not pages. */
  aggregate?: AggregateSpec
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

const OP_TOKENS: Array<[RegExp, QueryOp]> = [
  [/^>=\s*/, 'gte'],
  [/^<=\s*/, 'lte'],
  [/^!=\s*/, 'ne'],
  [/^>\s*/, 'gt'],
  [/^<\s*/, 'lt'],
  [/^contains\s+/i, 'contains'],
]

// Route one `key: value` pair into either containment (`where`) or an operator
// condition (`filters`). A value that opens with an operator token (`> 100`,
// `contains prod`) or is bare `exists` becomes a filter; anything else stays
// exact containment, so v1 blocks are unchanged.
function assignPair(
  key: string,
  rawVal: string,
  where: Record<string, unknown>,
  filters: QueryFilter[],
) {
  const k = unquote(key)
  if (k === '') return
  // Unquote FIRST: an operator value is usually quoted in YAML (`cost: "> 100"`),
  // so the operator token only shows once the quotes are stripped.
  const v = unquote(rawVal.trim())
  if (/^exists$/i.test(v)) {
    filters.push({ key: k, op: 'exists' })
    return
  }
  for (const [re, op] of OP_TOKENS) {
    if (re.test(v)) {
      const rest = v.replace(re, '').trim()
      filters.push({
        key: k,
        op,
        value: op === 'contains' ? rest : scalar(rest),
      })
      return
    }
  }
  where[k] = scalar(v)
}

// Split a `k: v` list into where/filters. Used for both an inline `{…}` flow map
// and a block-indented mapping.
function assignPairs(
  pairs: string[],
  where: Record<string, unknown>,
  filters: QueryFilter[],
) {
  for (const pair of pairs) {
    const idx = pair.indexOf(':')
    if (idx < 0) continue
    assignPair(pair.slice(0, idx), pair.slice(idx + 1), where, filters)
  }
}

// Parse a sort value into ordered terms. Accepts the v1 forms (`-updated`,
// `title`) and v2 multi-key with per-key direction (`cost desc, title asc`).
function parseOrder(val: string): QuerySort[] {
  const out: QuerySort[] = []
  for (const term of splitItems(val)) {
    const t = unquote(term).trim()
    if (t === '') continue
    const parts = t.split(/\s+/)
    if (parts.length >= 2) {
      out.push({ field: parts[0], dir: /^desc$/i.test(parts[1]) ? 'desc' : 'asc' })
    } else if (t.startsWith('-')) {
      out.push({ field: t.slice(1), dir: 'desc' })
    } else {
      out.push({ field: t, dir: 'asc' })
    }
  }
  return out
}

const AGG_FNS = new Set(['sum', 'count', 'avg', 'min', 'max'])

// Parse an aggregate value into functions. Accepts `sum(cost) as total`,
// `count as n`, `avg(cost)`, comma-separated. Returns null on any malformed item
// so the caller can surface an error rather than silently drop it.
function parseAggFns(val: string): AggFn[] | null {
  const out: AggFn[] = []
  for (const item of splitItems(val)) {
    const t = unquote(item).trim()
    const withKey = t.match(/^(\w+)\s*\(\s*([^)]*?)\s*\)(?:\s+as\s+(\w+))?$/i)
    const bare = t.match(/^(count)(?:\s+as\s+(\w+))?$/i)
    if (withKey) {
      const fn = withKey[1].toLowerCase()
      if (!AGG_FNS.has(fn)) return null
      out.push({
        fn: fn as AggFn['fn'],
        key: withKey[2] || undefined,
        as: withKey[3] || fn,
      })
    } else if (bare) {
      out.push({ fn: 'count', as: bare[2] || 'count' })
    } else {
      return null
    }
  }
  return out.length ? out : null
}

// Detect a computed column `<prop> <op> <number> as <alias>`. Returns null for a
// plain column name. The expression is a single prop, one arithmetic op, one
// numeric literal — evaluated client-side, never sent to SQL.
function parseComputed(col: string): ComputedColumn | null {
  const m = col.match(/^\s*(\w+)\s*([-+*/])\s*(-?\d+(?:\.\d+)?)\s+as\s+(\w+)\s*$/i)
  if (!m) return null
  return {
    prop: m[1],
    op: m[2] as ComputedColumn['op'],
    literal: parseFloat(m[3]),
    alias: m[4],
  }
}

export function parseQuerySpec(code: string): QuerySpec | QuerySpecError {
  const lines = code.split('\n')
  const where: Record<string, unknown> = {}
  const filters: QueryFilter[] = []
  let target: QueryTarget = 'pages'
  let space: 'here' | number | undefined
  let page: 'here' | number | undefined
  let columns: string[] = []
  let sort: string | undefined
  let order: QuerySort[] | undefined
  let aggregate: AggregateSpec | undefined
  let groupBy: string | undefined
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
          assignPairs(splitItems(body), where, filters)
        } else if (val === '') {
          // Block mapping: consume following indented `  k: v` lines.
          const block: string[] = []
          while (i + 1 < lines.length && /^\s+\S/.test(lines[i + 1])) {
            block.push(lines[i + 1].trim())
            i++
          }
          assignPairs(block, where, filters)
        } else {
          return { error: 'where must be a mapping' }
        }
        break
      }
      case 'target': {
        const tv = unquote(val).toLowerCase()
        if (tv === 'pages' || tv === 'comments') target = tv
        else return { error: `invalid target "${val}" (pages | comments)` }
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
      case 'page': {
        const pv = unquote(val).toLowerCase()
        if (pv === '' || pv === 'all') page = undefined
        else if (pv === 'here') page = 'here'
        else if (/^\d+$/.test(pv)) page = parseInt(pv, 10)
        else return { error: `invalid page "${val}"` }
        break
      }
      case 'columns':
        columns = parseInlineList(val)
        break
      case 'sort':
        sort = unquote(val)
        order = parseOrder(val)
        break
      case 'aggregate': {
        const fns = parseAggFns(val)
        if (!fns) return { error: `invalid aggregate "${val}"` }
        aggregate = { fns }
        break
      }
      case 'group by':
      case 'group_by':
      case 'groupby':
        groupBy = unquote(val)
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

  if (aggregate && groupBy) aggregate.group_by = groupBy

  // Split display columns into plain names and computed expressions. A computed
  // alias stays in `columns` (so it renders a header) and gains an entry in
  // `computed` (so the cell knows how to evaluate it).
  const computed: ComputedColumn[] = []
  columns = columns.map((col) => {
    const c = parseComputed(col)
    if (c) {
      computed.push(c)
      return c.alias
    }
    return col
  })

  // Target-aware defaults (list mode only): a comment row has no title, and its
  // headline is the change_summary prop (NOT `summary` — the page abstract).
  if (columns.length === 0 && !aggregate) {
    columns =
      target === 'comments'
        ? ['change_summary', 'author', 'created']
        : ['title', 'updated']
  }
  return {
    target,
    where,
    filters,
    space,
    page,
    columns,
    computed,
    sort,
    order,
    aggregate,
    limit,
  }
}

// A compact one-line description of the query for the editor preview.
const OP_SIGNS: Record<QueryOp, string> = {
  gt: '>',
  lt: '<',
  gte: '>=',
  lte: '<=',
  ne: '!=',
  contains: '∋',
  exists: '?',
}

function summarize(spec: QuerySpec): string {
  const parts: string[] = []

  if (spec.aggregate) {
    const fns = spec.aggregate.fns
      .map((f) => (f.fn === 'count' ? 'count' : `${f.fn}(${f.key})`))
      .join(', ')
    let agg = fns
    if (spec.aggregate.group_by) agg += ` by ${spec.aggregate.group_by}`
    parts.push(agg)
  }

  const keys = Object.keys(spec.where)
  const noun = spec.target === 'comments' ? 'comments' : 'pages'
  const conds = [
    ...keys.map((k) => `${k}=${String(spec.where[k])}`),
    ...spec.filters.map((f) =>
      f.op === 'exists'
        ? `${f.key}?`
        : `${f.key}${OP_SIGNS[f.op]}${String(f.value)}`,
    ),
  ]
  parts.push(conds.length ? `${noun}: ${conds.join(', ')}` : `all ${noun}`)

  if (spec.space === 'here') parts.push('this space')
  else if (typeof spec.space === 'number') parts.push(`space ${spec.space}`)
  if (spec.page === 'here') parts.push('this page')
  else if (typeof spec.page === 'number') parts.push(`page ${spec.page}`)
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
