import { describe, it, expect } from 'vitest'
import { parseQuerySpec, isQueryError, type QuerySpec } from './query-spec'

function ok(code: string): QuerySpec {
  const s = parseQuerySpec(code)
  if (isQueryError(s)) throw new Error(`unexpected parse error: ${s.error}`)
  return s
}

describe('parseQuerySpec v2', () => {
  it('keeps bare values as containment (v1 back-compat)', () => {
    const s = ok('where:\n  type: incident\n  status: active')
    expect(s.where).toEqual({ type: 'incident', status: 'active' })
    expect(s.filters).toEqual([])
  })

  it('routes operator values into filters, leaving bare values in where', () => {
    const s = ok('where:\n  type: run\n  cost: "> 100"\n  score: ">= 4"\n  n: "< 2"\n  model: "!= gpt-4"')
    expect(s.where).toEqual({ type: 'run' }) // only the bare value stays containment
    expect(s.filters).toEqual([
      { key: 'cost', op: 'gt', value: 100 },
      { key: 'score', op: 'gte', value: 4 },
      { key: 'n', op: 'lt', value: 2 },
      { key: 'model', op: 'ne', value: 'gpt-4' },
    ])
  })

  it('parses contains and bare exists', () => {
    const s = ok('where:\n  tags: "contains prod"\n  owner: exists')
    expect(s.filters).toEqual([
      { key: 'tags', op: 'contains', value: 'prod' },
      { key: 'owner', op: 'exists' },
    ])
    expect(s.where).toEqual({})
  })

  it('parses multi-key sort with per-key direction', () => {
    const s = ok('sort: cost desc, title asc')
    expect(s.order).toEqual([
      { field: 'cost', dir: 'desc' },
      { field: 'title', dir: 'asc' },
    ])
  })

  it('still parses the v1 -prefix sort form', () => {
    const s = ok('sort: -updated')
    expect(s.order).toEqual([{ field: 'updated', dir: 'desc' }])
    expect(s.sort).toBe('-updated')
  })

  it('parses aggregate functions and group by', () => {
    const s = ok('aggregate: sum(cost) as total, count as n\ngroup by: model')
    expect(s.aggregate).toEqual({
      fns: [
        { fn: 'sum', key: 'cost', as: 'total' },
        { fn: 'count', as: 'n' },
      ],
      group_by: 'model',
    })
  })

  it('rejects an unknown aggregate function', () => {
    const s = parseQuerySpec('aggregate: median(cost)')
    expect(isQueryError(s)).toBe(true)
  })

  it('extracts a computed column and keeps its alias as a header', () => {
    const s = ok('columns: [title, "cost * 1000 as mills"]')
    expect(s.columns).toEqual(['title', 'mills'])
    expect(s.computed).toEqual([
      { prop: 'cost', op: '*', literal: 1000, alias: 'mills' },
    ])
  })
})
