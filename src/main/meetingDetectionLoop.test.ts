import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CalendarEvent, Note } from '../shared/types'
import { MeetingDetectionManager } from './meetingDetectionLoop'

function meetingEvent(): CalendarEvent {
  const now = new Date()
  return {
    id: 'event-1',
    title: 'Weekly sync',
    starts_at: new Date(now.getTime() - 5 * 60 * 1000).toISOString(),
    ends_at: new Date(now.getTime() + 25 * 60 * 1000).toISOString(),
    description: 'Team sync',
    location: 'Conference room',
    conferencing_url: 'https://meet.example/weekly-sync',
    attendees: [],
    source_id: 'calendar-1',
  }
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

const managers: MeetingDetectionManager[] = []

function createManager(autoRecordDetectedMeetings: boolean, hasWindowInitial = false) {
  let hasWindow = hasWindowInitial
  const event = meetingEvent()
  const getCalendarEvents = vi.fn().mockResolvedValue([event])
  const listNotes = vi.fn().mockResolvedValue(notes)
  const createAutoRecordNote = vi.fn(async () => 'note-created')
  const sendPromptShow = vi.fn()
  const sendPromptClear = vi.fn()
  const sendAutoRecord = vi.fn()
  const showNotification = vi.fn((_payload, onClick: () => void) => {
    onClick()
  })
  const ensureWindow = vi.fn(() => {
    hasWindow = true
    return null
  })
  const manager = new MeetingDetectionManager({
    getCalendarEvents,
    listNotes,
    getCalendarPrefs: vi.fn(() => ({ autoRecordDetectedMeetings })),
    createAutoRecordNote,
    ensureWindow,
    hasWindow: () => hasWindow,
    focusWindow: vi.fn(),
    sendPromptShow,
    sendPromptClear,
    sendAutoRecord,
    showNotification,
    log: vi.fn(),
  })
  managers.push(manager)
  return { manager, event, ensureWindow, sendPromptShow, sendPromptClear, sendAutoRecord, showNotification, getCalendarEvents, listNotes }
}

async function flush() {
  await Promise.resolve()
  await Promise.resolve()
}

afterEach(() => {
  while (managers.length) {
    managers.pop()?.stop()
  }
  vi.useRealTimers()
  vi.clearAllMocks()
})

beforeEach(() => {
  vi.useFakeTimers()
})

describe('MeetingDetectionManager', () => {
  it('creates a window and triggers capture when auto-record is enabled without an open window', async () => {
    const { manager, ensureWindow, sendAutoRecord, sendPromptShow } = createManager(true, false)

    manager.start()
    await flush()
    await manager.rendererReadyForWindow()
    await flush()

    expect(ensureWindow).toHaveBeenCalledTimes(1)
    expect(sendAutoRecord).toHaveBeenCalledTimes(1)
    expect(sendAutoRecord).toHaveBeenCalledWith({ noteId: 'note-created' })
    expect(sendPromptShow).not.toHaveBeenCalled()

    manager.stop()
  })

  it('posts a notification and does not start capture when auto-record is off and no window exists', async () => {
    const { manager, ensureWindow, sendAutoRecord, showNotification } = createManager(false, false)

    manager.start()
    await flush()

    expect(ensureWindow).not.toHaveBeenCalled()
    expect(showNotification).toHaveBeenCalledTimes(1)
    expect(sendAutoRecord).not.toHaveBeenCalled()

    manager.stop()
  })

  it('shows the prompt once when a window is open and suppresses duplicate handling', async () => {
    const { manager, event, sendPromptShow, sendPromptClear, sendAutoRecord } = createManager(false, true)

    await manager.rendererReadyForWindow()
    manager.start()
    await flush()

    expect(sendPromptShow).toHaveBeenCalledTimes(1)
    expect(sendAutoRecord).not.toHaveBeenCalled()

    const key = `${event.id}::${event.starts_at}`
    manager.acceptPrompt(key)
    await vi.advanceTimersByTimeAsync(45_000)
    await flush()

    expect(sendPromptClear).toHaveBeenCalledWith({ occurrenceKey: key })
    expect(sendPromptShow).toHaveBeenCalledTimes(1)
    expect(sendAutoRecord).not.toHaveBeenCalled()

    manager.stop()
  })

  it('continues detection after the window closes and reopens', async () => {
    let hasWindow = true
    const sendPromptShow = vi.fn()
    const event = meetingEvent()
    const getCalendarEvents = vi.fn().mockResolvedValue([event])
    const manager = new MeetingDetectionManager({
      getCalendarEvents,
      listNotes: vi.fn().mockResolvedValue(notes),
      getCalendarPrefs: vi.fn(() => ({ autoRecordDetectedMeetings: false })),
      createAutoRecordNote: vi.fn(),
      ensureWindow: vi.fn(() => {
        hasWindow = true
        return null
      }),
      hasWindow: () => hasWindow,
      focusWindow: vi.fn(),
      sendPromptShow,
      sendPromptClear: vi.fn(),
      sendAutoRecord: vi.fn(),
      showNotification: vi.fn(),
      log: vi.fn(),
    })

    await manager.rendererReadyForWindow()
    manager.start()
    await flush()

    expect(sendPromptShow).toHaveBeenCalledTimes(1)

    manager.windowClosed()
    hasWindow = false

    await vi.advanceTimersByTimeAsync(45_000)
    await flush()

    expect(getCalendarEvents).toHaveBeenCalledTimes(2)
    expect(sendPromptShow).toHaveBeenCalledTimes(1)

    await manager.rendererReadyForWindow()
    await flush()

    expect(sendPromptShow).toHaveBeenCalledTimes(2)

    manager.stop()
  })
})
