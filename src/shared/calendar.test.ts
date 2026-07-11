import { describe, expect, it } from 'vitest'
import { buildCalendarEventsPath } from './calendar'

describe('buildCalendarEventsPath', () => {
  it('builds the calendar events path with encoded RFC3339 timestamps', () => {
    expect(buildCalendarEventsPath('2026-07-01T00:00:00Z', '2026-07-31T23:59:59Z')).toBe(
      '/api/calendar/events?from=2026-07-01T00%3A00%3A00Z&to=2026-07-31T23%3A59%3A59Z',
    )
  })

  it('encodes characters like + and : from a timezone-offset timestamp', () => {
    expect(buildCalendarEventsPath('2026-07-09T10:00:00+02:00', '2026-07-09T18:00:00+02:00')).toBe(
      '/api/calendar/events?from=2026-07-09T10%3A00%3A00%2B02%3A00&to=2026-07-09T18%3A00%3A00%2B02%3A00',
    )
  })
})
