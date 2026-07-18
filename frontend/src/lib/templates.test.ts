import { describe, it, expect } from 'vitest'
import { extractVars, substituteVars } from './templates'

describe('extractVars', () => {
  it('pulls distinct names in first-seen order, deduped', () => {
    expect(extractVars('# {{title}}\n\nby {{author}} on {{date}}; see {{title}}')).toEqual([
      'title',
      'author',
      'date',
    ])
  })

  it('trims whitespace inside the braces and allows spaces in names', () => {
    expect(extractVars('Hello {{ first name }}!')).toEqual(['first name'])
  })

  it('returns [] for a body with no tokens', () => {
    expect(extractVars('plain markdown, no braces')).toEqual([])
  })

  it('ignores empty braces', () => {
    expect(extractVars('a {{}} b {{ }} c {{real}}')).toEqual(['real'])
  })
})

describe('substituteVars', () => {
  it('replaces every occurrence, leaving no {{ residue', () => {
    const out = substituteVars('{{greeting}}, {{name}}. Bye {{name}}.', {
      greeting: 'Hi',
      name: 'Ada',
    })
    expect(out).toBe('Hi, Ada. Bye Ada.')
    expect(out).not.toContain('{{')
  })

  it('substitutes a provided-but-empty value (not residue)', () => {
    expect(substituteVars('x{{gap}}y', { gap: '' })).toBe('xy')
  })

  it('leaves a token with no provided value intact rather than blanking it', () => {
    expect(substituteVars('keep {{unknown}}', {})).toBe('keep {{unknown}}')
  })

  it('round-trips: every extracted var, when filled, erases all tokens', () => {
    const body = '# {{h}}\n\n- {{a}}\n- {{b}} and {{a}}'
    const vars = extractVars(body)
    const filled = Object.fromEntries(vars.map((v) => [v, `val-${v}`]))
    const out = substituteVars(body, filled)
    expect(out).not.toMatch(/\{\{|\}\}/)
    expect(out).toBe('# val-h\n\n- val-a\n- val-b and val-a')
  })
})
