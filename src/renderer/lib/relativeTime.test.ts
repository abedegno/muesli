import { describe, it, expect } from 'vitest'
import { relativeTime } from './relativeTime'

const NOW = new Date(2026, 6, 13, 12, 0, 0)

const ago = (ms: number): Date => new Date(NOW.getTime() - ms)
const on = (y: number, m: number, d: number, h = 9): Date => new Date(y, m, d, h)

describe('relativeTime', () => {
  it('labels sub-minute times as just now', () => {
    expect(relativeTime(ago(0), NOW)).toBe('just now')
    expect(relativeTime(ago(59_000), NOW)).toBe('just now')
  })

  it('switches to minutes at 60 seconds and stays minute-based under an hour', () => {
    expect(relativeTime(ago(60_000), NOW)).toBe('1m ago')
    expect(relativeTime(ago(59 * 60_000), NOW)).toBe('59m ago')
  })

  it('switches to hours at 60 minutes and stays hour-based under a day', () => {
    expect(relativeTime(ago(60 * 60_000), NOW)).toBe('1h ago')
    expect(relativeTime(ago(23 * 60 * 60_000), NOW)).toBe('23h ago')
  })

  it('switches to days at 24 hours and stays day-based under a week', () => {
    expect(relativeTime(ago(24 * 60 * 60_000), NOW)).toBe('1d ago')
    expect(relativeTime(ago(6 * 24 * 60 * 60_000), NOW)).toBe('6d ago')
  })

  it('falls back to a short absolute date at a week and beyond', () => {
    expect(relativeTime(ago(7 * 24 * 60 * 60_000), NOW)).toBe('6 Jul')
    expect(relativeTime(on(2026, 5, 12), NOW)).toBe('12 Jun')
  })

  it('accepts an ISO string input', () => {
    expect(relativeTime(ago(59_000).toISOString(), NOW)).toBe('just now')
  })
})
