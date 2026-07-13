import { describe, expect, it } from 'vitest'
import { datetimeLocalToISOString } from './timeRange'

describe('datetimeLocalToISOString', () => {
  it('returns undefined for empty and invalid values', () => {
    expect(datetimeLocalToISOString('')).toBeUndefined()
    expect(datetimeLocalToISOString('  ')).toBeUndefined()
    expect(datetimeLocalToISOString('not-a-date')).toBeUndefined()
  })

  it('normalizes a valid datetime to ISO format', () => {
    const result = datetimeLocalToISOString('2026-07-13T12:34:56Z')
    expect(result).toBe('2026-07-13T12:34:56.000Z')
  })
})
