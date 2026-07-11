import { describe, it, expect, afterEach } from 'vitest'
import { groupEventsByDay } from './comingUp'
import type { CalendarEvent } from '../../shared/types'

const ev = (over: Partial<CalendarEvent>): CalendarEvent => ({
  id: 'id', title: 'Event', starts_at: '', ends_at: '', description: '', location: '',
  conferencing_url: '', attendees: [], source_id: 'src', ...over,
})

// Fixed "now" so tests are deterministic regardless of the machine's clock.
// Wednesday 2026-07-09, 09:00 local.
const NOW = new Date(2026, 6, 9, 9, 0, 0)

describe('groupEventsByDay', () => {
  it('returns exactly 7 days, today through today+6, in chronological order', () => {
    const groups = groupEventsByDay([], NOW)
    expect(groups).toHaveLength(7)
    expect(groups.map((g) => g.date)).toEqual([
      '2026-07-09', '2026-07-10', '2026-07-11', '2026-07-12',
      '2026-07-13', '2026-07-14', '2026-07-15',
    ])
  })

  it('groups events spanning multiple distinct days into the right day buckets', () => {
    const a = ev({ id: 'a', starts_at: '2026-07-09T10:00:00', ends_at: '2026-07-09T10:30:00' })
    const b = ev({ id: 'b', starts_at: '2026-07-11T14:00:00', ends_at: '2026-07-11T14:30:00' })
    const groups = groupEventsByDay([a, b], NOW)
    expect(groups.find((g) => g.date === '2026-07-09')?.events.map((e) => e.id)).toEqual(['a'])
    expect(groups.find((g) => g.date === '2026-07-11')?.events.map((e) => e.id)).toEqual(['b'])
  })

  it('sorts multiple events on the same day chronologically by starts_at', () => {
    const late = ev({ id: 'late', starts_at: '2026-07-09T15:00:00', ends_at: '2026-07-09T15:30:00' })
    const early = ev({ id: 'early', starts_at: '2026-07-09T09:30:00', ends_at: '2026-07-09T10:00:00' })
    const mid = ev({ id: 'mid', starts_at: '2026-07-09T12:00:00', ends_at: '2026-07-09T12:30:00' })
    const groups = groupEventsByDay([late, early, mid], NOW)
    const day = groups.find((g) => g.date === '2026-07-09')
    expect(day?.events.map((e) => e.id)).toEqual(['early', 'mid', 'late'])
  })

  it('still includes a day with zero events as an empty array', () => {
    const onlyOne = ev({ id: 'only', starts_at: '2026-07-09T10:00:00', ends_at: '2026-07-09T10:30:00' })
    const groups = groupEventsByDay([onlyOne], NOW)
    const emptyDay = groups.find((g) => g.date === '2026-07-12')
    expect(emptyDay).toBeDefined()
    expect(emptyDay?.events).toEqual([])
  })

  it('excludes events outside the 7-day window (past and beyond day 7)', () => {
    const past = ev({ id: 'past', starts_at: '2026-07-08T10:00:00', ends_at: '2026-07-08T10:30:00' })
    const farFuture = ev({ id: 'far', starts_at: '2026-07-20T10:00:00', ends_at: '2026-07-20T10:30:00' })
    const groups = groupEventsByDay([past, farFuture], NOW)
    const allIds = groups.flatMap((g) => g.events.map((e) => e.id))
    expect(allIds).toEqual([])
  })
})

describe('groupEventsByDay — day-window invariants (regression: DST-safe day arithmetic)', () => {
  const originalTz = process.env.TZ

  afterEach(() => {
    process.env.TZ = originalTz
  })

  // Table of (tz, y, m, d, h) tuples that should each produce exactly 7
  // unique, consecutive local calendar days. `now` is constructed *inside*
  // each test after `process.env.TZ` is set, so the Date's calendar fields
  // are unambiguous regardless of the host machine's default timezone.
  // Includes real DST transitions in America/New_York (spring-forward loses
  // an hour on 2026-03-08; fall-back gains an hour on 2026-11-01) and
  // Europe/London (clocks back on 2026-10-25) — building each day via a
  // fixed 24h millisecond offset from local midnight can drift across the
  // transition and produce a duplicate `date` (collapsing two distinct days
  // into one slot and silently dropping the window's last day).
  const cases: { label: string; tz: string; y: number; m: number; d: number; h: number }[] = [
    { label: 'ordinary week, no DST transition', tz: 'America/New_York', y: 2026, m: 6, d: 9, h: 9 },
    { label: 'window spanning US spring-forward (2026-03-08)', tz: 'America/New_York', y: 2026, m: 2, d: 5, h: 9 },
    { label: 'window spanning US fall-back (2026-11-01)', tz: 'America/New_York', y: 2026, m: 9, d: 29, h: 9 },
    { label: 'window spanning UK clocks-back (2026-10-25)', tz: 'Europe/London', y: 2026, m: 9, d: 22, h: 9 },
  ]

  it.each(cases)('$label: returns exactly 7 unique, consecutive calendar days', ({ tz, y, m, d, h }) => {
    process.env.TZ = tz
    const now = new Date(y, m, d, h, 0, 0)
    const groups = groupEventsByDay([], now)
    const dates = groups.map((g) => g.date)
    expect(dates).toHaveLength(7)
    expect(new Set(dates).size).toBe(7)
    // Consecutive: each entry is exactly one calendar day after the previous.
    for (let i = 1; i < dates.length; i++) {
      const [py, pm, pd] = dates[i - 1].split('-').map(Number)
      const expectedNext = new Date(py, pm - 1, pd + 1)
      const [cy, cm, cd] = dates[i].split('-').map(Number)
      expect([cy, cm - 1, cd]).toEqual([expectedNext.getFullYear(), expectedNext.getMonth(), expectedNext.getDate()])
    }
  })

  it('fall-back regression: 2026-11-01 (US Eastern) appears exactly once, and the window still reaches day 7 (2026-11-04)', () => {
    process.env.TZ = 'America/New_York'
    const now = new Date(2026, 9, 29, 9, 0, 0) // Oct 29, 2026
    const dates = groupEventsByDay([], now).map((g) => g.date)
    expect(dates).toEqual([
      '2026-10-29', '2026-10-30', '2026-10-31', '2026-11-01',
      '2026-11-02', '2026-11-03', '2026-11-04',
    ])
  })
})
