import { useCallback, useEffect, useRef, useState } from 'react'
import type { NavigateFunction } from 'react-router-dom'
import { muesli } from '@/api'
import { detectActiveMeeting } from '@/lib/meetingDetect'
import { decideMeetingDetectionAction, meetingOccurrenceKey } from '@/lib/meetingDetectionLoop'
import { loadCalendarPrefs } from '@/lib/calendarPrefs'
import type { CalendarEvent, Note } from '../../shared/types'

const LOOP_INTERVAL_MS = 45_000
const WINDOW_BACK_MS = 2 * 60 * 60 * 1000
const WINDOW_FORWARD_MS = 30 * 60 * 1000

interface UseMeetingDetectionLoopArgs {
  notes: Note[]
  loaded: boolean
  navigate: NavigateFunction
  notify: (message: string, tone?: 'error' | 'info') => void
  refresh: () => void
}

async function createLinkedMeetingNote(
  event: CalendarEvent,
  navigate: NavigateFunction,
  notify: (message: string, tone?: 'error' | 'info') => void,
  refresh: () => void,
): Promise<void> {
  try {
    const note = await muesli.createNote(event.title)
    await muesli.linkNoteEvent(note.id, event.id)
    refresh()
    navigate(`/notes/${note.id}?capture=1&autostart=1`, { replace: true })
  } catch (err) {
    notify(err instanceof Error ? err.message : 'Could not start meeting recording', 'error')
  }
}

export function useMeetingDetectionLoop({ notes, loaded, navigate, notify, refresh }: UseMeetingDetectionLoopArgs) {
  const [promptEvent, setPromptEvent] = useState<CalendarEvent | null>(null)
  const promptKeyRef = useRef<string | null>(null)
  const surfacedOccurrencesRef = useRef<Set<string>>(new Set())
  const dismissedOccurrencesRef = useRef<Set<string>>(new Set())
  const inFlightRef = useRef(false)

  const clearPrompt = useCallback(() => {
    promptKeyRef.current = null
    setPromptEvent(null)
  }, [])

  const markPrompt = useCallback((event: CalendarEvent) => {
    const key = meetingOccurrenceKey(event)
    if (promptKeyRef.current === key) return
    promptKeyRef.current = key
    setPromptEvent(event)
  }, [])

  const handleDetection = useCallback(async () => {
    if (!loaded || inFlightRef.current) return
    inFlightRef.current = true
    try {
      const now = new Date()
      const from = new Date(now.getTime() - WINDOW_BACK_MS).toISOString()
      const to = new Date(now.getTime() + WINDOW_FORWARD_MS).toISOString()
      let events: CalendarEvent[]
      try {
        events = await muesli.getCalendarEvents(from, to)
      } catch {
        clearPrompt()
        return
      }

      if (!events.length) {
        clearPrompt()
        return
      }

      const prefs = loadCalendarPrefs()
      const active = detectActiveMeeting(events, now, prefs)
      const decision = decideMeetingDetectionAction({
        detectedEvent: active,
        dismissedOccurrences: dismissedOccurrencesRef.current,
        surfacedOccurrences: surfacedOccurrencesRef.current,
        alreadyRecording: notes.some((n) => n.status === 'recording'),
        autoRecordDetectedMeetings: prefs.autoRecordDetectedMeetings,
      })

      if (decision.action === 'auto-record' && decision.event && decision.occurrenceKey) {
        surfacedOccurrencesRef.current.add(decision.occurrenceKey)
        clearPrompt()
        await createLinkedMeetingNote(decision.event, navigate, notify, refresh)
        return
      }

      if (decision.action === 'prompt' && decision.event && decision.occurrenceKey) {
        markPrompt(decision.event)
        return
      }

      clearPrompt()
    } finally {
      inFlightRef.current = false
    }
  }, [clearPrompt, loaded, markPrompt, navigate, notes, notify, refresh])

  useEffect(() => {
    void handleDetection()
    const interval = window.setInterval(() => { void handleDetection() }, LOOP_INTERVAL_MS)
    const onFocus = () => { void handleDetection() }
    window.addEventListener('focus', onFocus)
    return () => {
      clearInterval(interval)
      window.removeEventListener('focus', onFocus)
    }
  }, [handleDetection])

  const acceptPrompt = useCallback(async () => {
    if (!promptEvent) return
    const key = meetingOccurrenceKey(promptEvent)
    surfacedOccurrencesRef.current.add(key)
    clearPrompt()
    await createLinkedMeetingNote(promptEvent, navigate, notify, refresh)
  }, [clearPrompt, navigate, notify, promptEvent, refresh])

  const dismissPrompt = useCallback(() => {
    if (!promptEvent) return
    dismissedOccurrencesRef.current.add(meetingOccurrenceKey(promptEvent))
    clearPrompt()
  }, [clearPrompt, promptEvent])

  return {
    promptEvent,
    acceptPrompt,
    dismissPrompt,
  }
}
