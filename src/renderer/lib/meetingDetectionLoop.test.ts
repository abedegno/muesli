import { describe, expect, it } from 'vitest'
import type { CalendarEvent } from '../../shared/types'
import { decideMeetingDetectionAction, meetingOccurrenceKey } from './meetingDetectionLoop'

function event(overrides: Partial<CalendarEvent> = {}): CalendarEvent {
  return {
    id: 'event-1',
    title: 'Meeting',
    starts_at: '2026-07-09T14:00:00.000Z',
    ends_at: '2026-07-09T14:30:00.000Z',
    description: '',
    location: '',
    conferencing_url: 'https://meet.example/active',
    attendees: [],
    source_id: 'source-1',
    ...overrides,
  }
}

describe('meetingOccurrenceKey', () => {
  it('stabilizes identity on event id + starts_at', () => {
    expect(meetingOccurrenceKey(event())).toBe('event-1::2026-07-09T14:00:00.000Z')
  })
})

describe('decideMeetingDetectionAction', () => {
  it.each([
    {
      name: 'fresh detection prompts when auto-record is off',
      input: {
        detectedEvent: event(),
        dismissedOccurrences: new Set<string>(),
        surfacedOccurrences: new Set<string>(),
        alreadyRecording: false,
        autoRecordDetectedMeetings: false,
      },
      action: 'prompt',
    },
    {
      name: 'fresh detection auto-records when auto-record is on',
      input: {
        detectedEvent: event(),
        dismissedOccurrences: new Set<string>(),
        surfacedOccurrences: new Set<string>(),
        alreadyRecording: false,
        autoRecordDetectedMeetings: true,
      },
      action: 'auto-record',
    },
    {
      name: 'a surfaced occurrence never re-triggers',
      input: {
        detectedEvent: event(),
        dismissedOccurrences: new Set<string>(),
        surfacedOccurrences: new Set<string>(['event-1::2026-07-09T14:00:00.000Z']),
        alreadyRecording: false,
        autoRecordDetectedMeetings: true,
      },
      action: 'noop',
    },
    {
      name: 'a dismissed occurrence stays suppressed',
      input: {
        detectedEvent: event(),
        dismissedOccurrences: new Set<string>(['event-1::2026-07-09T14:00:00.000Z']),
        surfacedOccurrences: new Set<string>(),
        alreadyRecording: false,
        autoRecordDetectedMeetings: true,
      },
      action: 'noop',
    },
    {
      name: 'already-recording suppresses both prompt and auto-record',
      input: {
        detectedEvent: event(),
        dismissedOccurrences: new Set<string>(),
        surfacedOccurrences: new Set<string>(),
        alreadyRecording: true,
        autoRecordDetectedMeetings: true,
      },
      action: 'noop',
    },
    {
      name: 'no detected event is a no-op',
      input: {
        detectedEvent: null,
        dismissedOccurrences: new Set<string>(),
        surfacedOccurrences: new Set<string>(),
        alreadyRecording: false,
        autoRecordDetectedMeetings: true,
      },
      action: 'noop',
    },
    {
      name: 'a different occurrence of the same event id can surface after a dismissal',
      input: {
        detectedEvent: event({ starts_at: '2026-07-09T15:00:00.000Z', ends_at: '2026-07-09T15:30:00.000Z' }),
        dismissedOccurrences: new Set<string>(['event-1::2026-07-09T14:00:00.000Z']),
        surfacedOccurrences: new Set<string>(),
        alreadyRecording: false,
        autoRecordDetectedMeetings: false,
      },
      action: 'prompt',
    },
  ])('$name', ({ input, action }) => {
    const actual = decideMeetingDetectionAction(input)
    expect(actual.action).toBe(action)
  })

  it('routes fresh detected events to prompt when auto-record is gated off', () => {
    const actual = decideMeetingDetectionAction({
      detectedEvent: event(),
      dismissedOccurrences: new Set<string>(),
      surfacedOccurrences: new Set<string>(),
      alreadyRecording: false,
      autoRecordDetectedMeetings: true,
      allowAutoRecord: false,
    })

    expect(actual.action).toBe('prompt')
  })
})
