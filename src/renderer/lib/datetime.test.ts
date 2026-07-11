import { describe, it, expect } from 'vitest'
import { dayBucket, formatTime, formatEventTimeRange, groupNotesByDate, defaultMeetingTitle } from './datetime'
import type { CalendarEvent, Note } from '../../shared/types'

const NOW = new Date(2026, 5, 13, 12, 0, 0) // Sat Jun 13 2026, local
const note = (over: Partial<Note>): Note => ({
  id: '1', title: 't', status: 'ready', created_at: '', updated_at: '', partial_transcript: false, ...over,
} as Note)
const at = (y: number, m: number, d: number, h = 9): string => new Date(y, m, d, h).toISOString()

describe('dayBucket', () => {
  it('labels today / yesterday', () => {
    expect(dayBucket(at(2026, 5, 13, 8), NOW)).toBe('Today')
    expect(dayBucket(at(2026, 5, 12, 23), NOW)).toBe('Yesterday') // 11pm yesterday is still Yesterday
  })
  it('labels recent days by weekday, older by date', () => {
    expect(dayBucket(at(2026, 5, 10), NOW)).toBe('Wednesday') // 3 days back
    expect(dayBucket(at(2026, 5, 3), NOW)).toBe('Jun 3')       // 10 days back, same year
    expect(dayBucket(at(2025, 5, 3), NOW)).toBe('Jun 3, 2025') // prior year keeps the year
  })
  it('is robust to an empty/invalid date', () => {
    expect(dayBucket('', NOW)).toBe('Earlier')
  })
})

describe('formatTime', () => {
  it('formats a short local time', () => {
    expect(formatTime(new Date(2026, 5, 13, 17, 15).toISOString())).toMatch(/5:15/)
  })
  it('returns empty string for an invalid date', () => {
    expect(formatTime('')).toBe('')
  })
})

const calendarEvent = (over: Partial<CalendarEvent>): CalendarEvent => ({
  id: 'e1', title: 'Standup', starts_at: '', ends_at: '', description: '', location: '',
  conferencing_url: '', attendees: [], source_id: 's1', ...over,
} as CalendarEvent)

describe('formatEventTimeRange', () => {
  it('formats a start–end range', () => {
    const ev = calendarEvent({
      starts_at: new Date(2026, 5, 13, 14, 0).toISOString(),
      ends_at: new Date(2026, 5, 13, 14, 30).toISOString(),
    })
    expect(formatEventTimeRange(ev)).toMatch(/2:00.*2:30/)
  })
  it('falls back to just the start time when the end is invalid', () => {
    const ev = calendarEvent({ starts_at: new Date(2026, 5, 13, 14, 0).toISOString(), ends_at: '' })
    expect(formatEventTimeRange(ev)).toMatch(/2:00/)
    expect(formatEventTimeRange(ev)).not.toContain('–')
  })
  it('returns empty string when both times are invalid', () => {
    expect(formatEventTimeRange(calendarEvent({ starts_at: '', ends_at: '' }))).toBe('')
  })
})

describe('groupNotesByDate', () => {
  it('orders newest-first and buckets by day', () => {
    const notes = [
      note({ id: 'a', created_at: at(2026, 5, 12, 9) }),
      note({ id: 'b', created_at: at(2026, 5, 13, 8) }),
      note({ id: 'c', created_at: at(2026, 5, 13, 10) }),
    ]
    const groups = groupNotesByDate(notes, NOW)
    expect(groups.map((g) => g.label)).toEqual(['Today', 'Yesterday'])
    expect(groups[0].notes.map((n) => n.id)).toEqual(['c', 'b']) // within-day newest first
    expect(groups[1].notes.map((n) => n.id)).toEqual(['a'])
  })
  it('puts pinned notes in a leading bucket without duplicating date headers', () => {
    const groups = groupNotesByDate(
      [
        note({ id: 'today', created_at: at(2026, 5, 13, 9) }),
        note({ id: 'pinned-yesterday', created_at: at(2026, 5, 12, 9), pinned: true }),
        note({ id: 'yesterday', created_at: at(2026, 5, 12, 8) }),
      ],
      NOW,
    )

    expect(groups.map((g) => g.label)).toEqual(['Pinned', 'Today', 'Yesterday'])
    expect(groups[0].notes.map((n) => n.id)).toEqual(['pinned-yesterday'])
    expect(groups[1].notes.map((n) => n.id)).toEqual(['today'])
    expect(groups[2].notes.map((n) => n.id)).toEqual(['yesterday'])
  })
  it('returns [] for []', () => {
    expect(groupNotesByDate([], NOW)).toEqual([])
  })
})

describe('defaultMeetingTitle', () => {
  it('formats a clean "Meeting — Mon D, h:mm" title', () => {
    const t = defaultMeetingTitle(new Date(2026, 5, 13, 14, 10))
    expect(t).toMatch(/^Meeting — /)
    expect(t).toMatch(/Jun 13/)
    expect(t).toMatch(/2:10/)
  })
})

describe('groupNotesByDate — semantic buckets', () => {
  const NOW = new Date(2026, 6, 20, 12, 0, 0) // Mon Jul 20 2026, local
  const note = (over: Partial<Note>): Note => ({
    id: '1', title: 't', status: 'ready', created_at: '', updated_at: '', partial_transcript: false, ...over,
  } as Note)
  const at = (y: number, m: number, d: number, h = 9): string => new Date(y, m, d, h).toISOString()

  it('labels 7 days ago (Jul 13) as "Last week"', () => {
    const groups = groupNotesByDate([note({ id: 'a', created_at: at(2026, 6, 13) })], NOW)
    expect(groups[0].label).toBe('Last week')
  })

  it('labels 13 days ago (Jul 7) as "Last week"', () => {
    const groups = groupNotesByDate([note({ id: 'a', created_at: at(2026, 6, 7) })], NOW)
    expect(groups[0].label).toBe('Last week')
  })

  it('labels 14 days ago same month (Jul 6) as "This month"', () => {
    const groups = groupNotesByDate([note({ id: 'a', created_at: at(2026, 6, 6) })], NOW)
    expect(groups[0].label).toBe('This month')
  })

  it('labels 19 days ago same month (Jul 1) as "This month"', () => {
    const groups = groupNotesByDate([note({ id: 'a', created_at: at(2026, 6, 1) })], NOW)
    expect(groups[0].label).toBe('This month')
  })

  it('labels 20 days ago different month (Jun 30) as "Older"', () => {
    const groups = groupNotesByDate([note({ id: 'a', created_at: at(2026, 5, 30) })], NOW)
    expect(groups[0].label).toBe('Older')
  })

  it('labels exact diffDays=14 cross-month boundary as "Older"', () => {
    // NOW = Aug 14 2026; 14 days ago = Jul 31 (different month → Older)
    const nowAug14 = new Date(2026, 7, 14, 12, 0, 0)
    const groups = groupNotesByDate(
      [note({ id: 'x', created_at: new Date(2026, 6, 31, 9).toISOString() })], // Jul 31 2026
      nowAug14,
    )
    expect(groups[0].label).toBe('Older')
  })

  it('labels far past (Jan 1 2025) as "Older"', () => {
    const groups = groupNotesByDate([note({ id: 'a', created_at: at(2025, 0, 1) })], NOW)
    expect(groups[0].label).toBe('Older')
  })

  it('preserves Today / Yesterday / weekday buckets alongside older buckets', () => {
    const notes = [
      note({ id: 'today', created_at: at(2026, 6, 20, 10) }),
      note({ id: 'yesterday', created_at: at(2026, 6, 19, 9) }),
      note({ id: 'weekday', created_at: at(2026, 6, 16, 9) }), // Thu, 4 days ago
      note({ id: 'lastweek', created_at: at(2026, 6, 13, 9) }), // 7 days ago
    ]
    const groups = groupNotesByDate(notes, NOW)
    expect(groups.map((g) => g.label)).toEqual(['Today', 'Yesterday', 'Thursday', 'Last week'])
  })
})
