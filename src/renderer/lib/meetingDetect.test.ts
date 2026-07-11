import { describe, expect, it } from 'vitest'
import type { CalendarEvent } from '../../shared/types'
import { detectActiveMeeting } from './meetingDetect'

const baseNow = new Date('2026-07-09T14:15:00.000Z')

function event(overrides: Partial<CalendarEvent> = {}): CalendarEvent {
  return {
    id: 'event-1',
    title: 'Meeting',
    starts_at: '2026-07-09T14:00:00.000Z',
    ends_at: '2026-07-09T14:30:00.000Z',
    description: '',
    location: '',
    conferencing_url: '',
    attendees: [],
    source_id: 'source-1',
    ...overrides,
  }
}

const active = event({ conferencing_url: 'https://meet.example/active' })
const early = event({
  id: 'early',
  starts_at: '2026-07-09T13:00:00.000Z',
  ends_at: '2026-07-09T15:00:00.000Z',
  conferencing_url: 'https://meet.example/early',
})
const late = event({
  id: 'late',
  starts_at: '2026-07-09T14:00:00.000Z',
  ends_at: '2026-07-09T15:00:00.000Z',
  conferencing_url: 'https://meet.example/late',
})

describe('detectActiveMeeting', () => {
  it.each([
    {
      name: 'returns the active event with conferencing',
      events: [active],
      expected: active,
    },
    {
      name: 'returns null for an active event without conferencing',
      events: [event()],
      expected: null,
    },
    {
      name: 'returns null for an event that has not started yet',
      events: [
        event({
          starts_at: '2026-07-09T15:00:00.000Z',
          ends_at: '2026-07-09T15:30:00.000Z',
          conferencing_url: 'https://meet.example/future',
        }),
      ],
      expected: null,
    },
    {
      name: 'returns null for an event that already ended',
      events: [
        event({
          starts_at: '2026-07-09T13:00:00.000Z',
          ends_at: '2026-07-09T14:00:00.000Z',
          conferencing_url: 'https://meet.example/past',
        }),
      ],
      expected: null,
    },
    {
      name: 'prefers the most recently started active meeting when several qualify',
      events: [early, late],
      expected: late,
    },
  ])('$name', ({ events, expected }) => {
    const actual = detectActiveMeeting(events, baseNow, { autoRecordDetectedMeetings: false })
    expect(actual).toBe(expected)
  })
})
