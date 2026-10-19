import { describe, expect, it } from 'vitest'
import { formatDate, formatNumber, t } from './index'

describe('display messages and regional formatting', () => {
  it('interpolates whole messages so translations can reorder values', () => {
    expect(t('dates.meeting', { date: '18 Oct', time: '13:45' })).toBe('Meeting — 18 Oct, 13:45')
    expect(t('help.label', { label: 'Live transcript' })).toBe('Help: Live transcript')
  })
  it('uses the requested regional date order and number separators', () => {
    const date = new Date('2026-10-18T12:00:00Z')
    const options = { day: 'numeric', month: 'numeric', year: 'numeric', timeZone: 'UTC' } as const
    expect(formatDate(date, options, 'en-GB')).toBe('18/10/2026')
    expect(formatDate(date, options, 'en-US')).toBe('10/18/2026')
    expect(formatNumber(1234.5, {}, 'de-DE')).toBe('1.234,5')
    expect(formatNumber(1234.5, {}, 'en-GB')).toBe('1,234.5')
  })
})
