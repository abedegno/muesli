// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, renderHook } from '@testing-library/react'
import type { CalendarEvent } from '../../shared/types'
import { useMeetingDetectionLoop } from './useMeetingDetectionLoop'

const promptListeners = new Set<(payload: { event: CalendarEvent; occurrenceKey: string }) => void>()
const clearListeners = new Set<(payload: { occurrenceKey: string }) => void>()
const autoRecordListeners = new Set<(payload: { noteId: string }) => void>()

const mocks = vi.hoisted(() => ({
  createNote: vi.fn(),
  linkNoteEvent: vi.fn(),
  startNoteCapture: vi.fn(),
  meetingDetectionPromptAccept: vi.fn(),
  meetingDetectionPromptDismiss: vi.fn(),
  meetingDetectionRendererReady: vi.fn(),
}))

function emitPromptShow(payload: { event: CalendarEvent; occurrenceKey: string }) {
  for (const listener of promptListeners) listener(payload)
}

function emitPromptClear(payload: { occurrenceKey: string }) {
  for (const listener of clearListeners) listener(payload)
}

function emitAutoRecord(payload: { noteId: string }) {
  for (const listener of autoRecordListeners) listener(payload)
}

vi.mock('@/api', () => ({
  muesli: {
    createNote: mocks.createNote,
    linkNoteEvent: mocks.linkNoteEvent,
    startNoteCapture: mocks.startNoteCapture,
    meetingDetectionPromptAccept: mocks.meetingDetectionPromptAccept,
    meetingDetectionPromptDismiss: mocks.meetingDetectionPromptDismiss,
    meetingDetectionRendererReady: mocks.meetingDetectionRendererReady,
    onMeetingDetectionPromptShow: (listener: (payload: { event: CalendarEvent; occurrenceKey: string }) => void) => {
      promptListeners.add(listener)
      return () => promptListeners.delete(listener)
    },
    onMeetingDetectionPromptClear: (listener: (payload: { occurrenceKey: string }) => void) => {
      clearListeners.add(listener)
      return () => clearListeners.delete(listener)
    },
    onMeetingDetectionAutoRecord: (listener: (payload: { noteId: string }) => void) => {
      autoRecordListeners.add(listener)
      return () => autoRecordListeners.delete(listener)
    },
  },
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

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.restoreAllMocks()
  promptListeners.clear()
  clearListeners.clear()
  autoRecordListeners.clear()
})

beforeEach(() => {
  mocks.createNote.mockResolvedValue({ id: 'note-created' })
  mocks.linkNoteEvent.mockResolvedValue(undefined)
  mocks.startNoteCapture.mockResolvedValue({ id: 'note-created', status: 'recording' })
})

describe('useMeetingDetectionLoop', () => {
  it('subscribes to prompt show/clear events and forwards accept + dismiss back to main', async () => {
    const navigate = vi.fn()
    const notify = vi.fn()
    const refresh = vi.fn()

    const { result, unmount } = renderHook(() => useMeetingDetectionLoop({
      navigate,
      notify,
      refresh,
    }))

    await act(async () => {})
    expect(mocks.meetingDetectionRendererReady).toHaveBeenCalledTimes(1)

    await act(async () => {
      emitPromptShow({ event: meetingEvent, occurrenceKey: 'event-1::2026-07-11T14:00:00.000Z' })
    })
    expect(result.current.promptEvent).toEqual(meetingEvent)

    await act(async () => {
      await result.current.acceptPrompt()
    })

    expect(mocks.meetingDetectionPromptAccept).toHaveBeenCalledWith('event-1::2026-07-11T14:00:00.000Z')
    expect(mocks.createNote).toHaveBeenCalledWith('Weekly sync')
    expect(mocks.linkNoteEvent).toHaveBeenCalledWith('note-created', 'event-1')
    expect(mocks.startNoteCapture).toHaveBeenCalledWith('note-created')
    expect(navigate).toHaveBeenCalledWith('/notes/note-created?capture=1&autostart=1', { replace: true })
    expect(result.current.promptEvent).toBeNull()

    unmount()
  })

  it('dismisses a prompt and tells main which occurrence was dismissed', async () => {
    const navigate = vi.fn()
    const notify = vi.fn()
    const refresh = vi.fn()

    const { result } = renderHook(() => useMeetingDetectionLoop({
      navigate,
      notify,
      refresh,
    }))

    await act(async () => {})

    await act(async () => {
      emitPromptShow({ event: meetingEvent, occurrenceKey: 'event-1::2026-07-11T14:00:00.000Z' })
    })

    await act(async () => {
      result.current.dismissPrompt()
    })

    await act(async () => {
      emitPromptClear({ occurrenceKey: 'event-1::2026-07-11T14:00:00.000Z' })
    })

    expect(mocks.meetingDetectionPromptDismiss).toHaveBeenCalledWith('event-1::2026-07-11T14:00:00.000Z')
    expect(result.current.promptEvent).toBeNull()
  })

  it('starts capture directly when main emits an auto-record event', async () => {
    const navigate = vi.fn()
    const notify = vi.fn()
    const refresh = vi.fn()

    renderHook(() => useMeetingDetectionLoop({
      navigate,
      notify,
      refresh,
    }))

    await act(async () => {})

    await act(async () => {
      emitAutoRecord({ noteId: 'note-created' })
    })

    expect(mocks.createNote).not.toHaveBeenCalled()
    expect(mocks.linkNoteEvent).not.toHaveBeenCalled()
    expect(navigate).toHaveBeenCalledWith('/notes/note-created?capture=1&autostart=1', { replace: true })
  })

  it('removes listeners on unmount', async () => {
    const navigate = vi.fn()
    const notify = vi.fn()
    const refresh = vi.fn()

    const { unmount } = renderHook(() => useMeetingDetectionLoop({
      navigate,
      notify,
      refresh,
    }))

    await act(async () => {})
    expect(promptListeners.size).toBe(1)
    expect(clearListeners.size).toBe(1)
    expect(autoRecordListeners.size).toBe(1)

    unmount()

    expect(promptListeners.size).toBe(0)
    expect(clearListeners.size).toBe(0)
    expect(autoRecordListeners.size).toBe(0)
  })
})
