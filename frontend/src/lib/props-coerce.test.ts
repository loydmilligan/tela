import { describe, it, expect } from 'vitest'
import { coerceScalar, isEditableScalar } from './props-coerce'

describe('coerceScalar', () => {
  it('keeps numbers as numbers, not strings (the set_prop stringify lesson)', () => {
    expect(coerceScalar('42')).toBe(42)
    expect(coerceScalar('-7')).toBe(-7)
    expect(coerceScalar('3.5')).toBe(3.5)
    // The exact bug class: a number typed in the props editor must not land as "42".
    expect(typeof coerceScalar('42')).toBe('number')
  })

  it('coerces booleans and null', () => {
    expect(coerceScalar('true')).toBe(true)
    expect(coerceScalar('false')).toBe(false)
    expect(coerceScalar('null')).toBe(null)
  })

  it('leaves genuine strings as strings', () => {
    expect(coerceScalar('incident')).toBe('incident')
    expect(coerceScalar('gpt-4')).toBe('gpt-4')
    // A version-like token that is not a pure number stays a string.
    expect(coerceScalar('1.2.3')).toBe('1.2.3')
  })

  it('maps empty input to an empty string', () => {
    expect(coerceScalar('')).toBe('')
    expect(coerceScalar('   ')).toBe('')
  })
})

describe('isEditableScalar', () => {
  it('accepts scalars and null, rejects arrays and objects', () => {
    expect(isEditableScalar('x')).toBe(true)
    expect(isEditableScalar(42)).toBe(true)
    expect(isEditableScalar(true)).toBe(true)
    expect(isEditableScalar(null)).toBe(true)
    expect(isEditableScalar(['a', 'b'])).toBe(false)
    expect(isEditableScalar({ k: 1 })).toBe(false)
  })
})
