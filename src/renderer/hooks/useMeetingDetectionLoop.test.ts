// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, renderHook } from '@testing-library/react'
import type { CalendarEvent, Note } from '../../shared/types'
import { useMeetingDetectionLoop } from './useMeetingDetectionLoop'

const mocks = vi.hoisted(() => ({
  getCalendarEvents: vi.fn(),
  createNote: vi.fn(),
  linkNoteEvent: vi.fn(),
  detectActiveMeeting: vi.fn(),
  decideMeetingDetectionAction: vi.fn(),
  meetingOccurrenceKey: vi.fn(),
  loadCalendarPrefs: vi.fn(),
}))

vi.mock('@/api', () => ({
  muesli: {
    getCalendarEvents: mocks.getCalendarEvents,
    createNote: mocks.createNote,
    linkNoteEvent: mocks.linkNoteEvent,
  },
}))

vi.mock('@/lib/meetingDetect', () => ({
  detectActiveMeeting: mocks.detectActiveMeeting,
}))

vi.mock('@/lib/meetingDetectionLoop', () => ({
  decideMeetingDetectionAction: mocks.decideMeetingDetectionAction,
  meetingOccurrenceKey: mocks.meetingOccurrenceKey,
}))

vi.mock('@/lib/calendarPrefs', () => ({
  loadCalendarPrefs: mocks.loadCalendarPrefs,
}))

const meetingEvent: CalendarEvent = {
  id: 'event-1',
  title: 'Weekly sync',
  starts_at: '2026-07-11T14:00:00.000Z',
  ends_at: '2026-07-11T14:30:00.000Z',
  description: 'Team sync',
  location: 'Conference room',
  conferencing_url: 'https://meet.example/weekly-sync',
  attendees: [],
  source_id: 'calendar-1',
}

const notes: Note[] = [
  {
    id: 'note-1',
    title: 'Existing note',
    status: 'ready',
    created_at: '2026-07-11T13:00:00.000Z',
    updated_at: '2026-07-11T13:00:00.000Z',
    partial_transcript: false,
  },
]

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.restoreAllMocks()
  vi.useRealTimers()
})

beforeEach(() => {
  vi.useFakeTimers()
  mocks.loadCalendarPrefs.mockReturnValue({ autoRecordDetectedMeetings: false })
  mocks.detectActiveMeeting.mockReturnValue(meetingEvent)
  mocks.meetingOccurrenceKey.mockReturnValue('event-1::2026-07-11T14:00:00.000Z')
  mocks.createNote.mockResolvedValue({ id: 'note-created' })
  mocks.linkNoteEvent.mockResolvedValue(undefined)
})

describe('useMeetingDetectionLoop', () => {
  it('polls on the interval and surfaces a prompt for detected meetings', async () => {
    mocks.getCalendarEvents.mockResolvedValue([meetingEvent])
    mocks.decideMeetingDetectionAction.mockReturnValue({
      action: 'prompt',
      event: meetingEvent,
      occurrenceKey: 'event-1::2026-07-11T14:00:00.000Z',
    })

    const navigate = vi.fn()
    const notify = vi.fn()
    const refresh = vi.fn()

    const { result } = renderHook(() => useMeetingDetectionLoop({
      notes,
      loaded: true,
      navigate,
      notify,
      refresh,
    }))

    await act(async () => {})

    expect(mocks.getCalendarEvents).toHaveBeenCalledTimes(1)
    expect(result.current.promptEvent).toEqual(meetingEvent)
    expect(mocks.createNote).not.toHaveBeenCalled()
    expect(navigate).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(45_000)
    })

    expect(mocks.getCalendarEvents).toHaveBeenCalledTimes(2)
    expect(result.current.promptEvent).toEqual(meetingEvent)
  })

  it('does not detect while loaded=false and starts once loaded=true', async () => {
    mocks.getCalendarEvents.mockResolvedValue([meetingEvent])
    mocks.decideMeetingDetectionAction.mockReturnValue({
      action: 'prompt',
      event: meetingEvent,
      occurrenceKey: 'event-1::2026-07-11T14:00:00.000Z',
    })

    const navigate = vi.fn()
    const notify = vi.fn()
    const refresh = vi.fn()

    const { rerender, result } = renderHook(
      ({ loaded }) => useMeetingDetectionLoop({ notes, loaded, navigate, notify, refresh }),
      { initialProps: { loaded: false } },
    )

    await act(async () => {
      await vi.advanceTimersByTimeAsync(90_000)
    })

    expect(mocks.getCalendarEvents).not.toHaveBeenCalled()
    expect(result.current.promptEvent).toBeNull()

    rerender({ loaded: true })

    await act(async () => {})

    expect(mocks.getCalendarEvents).toHaveBeenCalledTimes(1)
    expect(result.current.promptEvent).toEqual(meetingEvent)
  })

  it('cleans up the interval and focus listener on unmount', async () => {
    mocks.getCalendarEvents.mockResolvedValue([meetingEvent])
    mocks.decideMeetingDetectionAction.mockReturnValue({
      action: 'prompt',
      event: meetingEvent,
      occurrenceKey: 'event-1::2026-07-11T14:00:00.000Z',
    })

    const navigate = vi.fn()
    const notify = vi.fn()
    const refresh = vi.fn()
    const clearIntervalSpy = vi.spyOn(globalThis, 'clearInterval')
    const removeEventListenerSpy = vi.spyOn(window, 'removeEventListener')

    const { unmount } = renderHook(() => useMeetingDetectionLoop({
      notes,
      loaded: true,
      navigate,
      notify,
      refresh,
    }))

    await act(async () => {})
    expect(mocks.getCalendarEvents).toHaveBeenCalledTimes(1)

    unmount()

    expect(clearIntervalSpy).toHaveBeenCalled()
    expect(removeEventListenerSpy).toHaveBeenCalledWith('focus', expect.any(Function))

    await act(async () => {
      await vi.advanceTimersByTimeAsync(45_000)
      window.dispatchEvent(new Event('focus'))
    })

    expect(mocks.getCalendarEvents).toHaveBeenCalledTimes(1)
  })
})
