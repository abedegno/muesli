import { useCallback, useEffect, useRef, useState } from 'react'
import type { NavigateFunction } from 'react-router-dom'
import { muesli } from '@/api'
import { meetingOccurrenceKey } from '../../shared/meetingDetectionLoop'
import type { CalendarEvent } from '../../shared/types'

interface UseMeetingDetectionLoopArgs {
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
    await muesli.startNoteCapture(note.id)
    refresh()
    navigate(`/notes/${note.id}?capture=1&autostart=1`, { replace: true })
  } catch (err) {
    notify(err instanceof Error ? err.message : 'Could not start meeting recording', 'error')
  }
}

export function useMeetingDetectionLoop({ navigate, notify, refresh }: UseMeetingDetectionLoopArgs) {
  const [promptEvent, setPromptEvent] = useState<CalendarEvent | null>(null)
  const promptKeyRef = useRef<string | null>(null)

  const clearPrompt = useCallback(() => {
    promptKeyRef.current = null
    setPromptEvent(null)
  }, [])

  const handlePromptShow = useCallback((payload: { event: CalendarEvent; occurrenceKey: string }) => {
    promptKeyRef.current = payload.occurrenceKey
    setPromptEvent(payload.event)
  }, [])

  const handlePromptClear = useCallback((payload: { occurrenceKey: string }) => {
    if (payload.occurrenceKey !== promptKeyRef.current) return
    clearPrompt()
  }, [clearPrompt])

  const handleAutoRecord = useCallback(async (payload: { noteId: string }) => {
    clearPrompt()
    try {
      await muesli.startNoteCapture(payload.noteId)
      navigate(`/notes/${payload.noteId}?capture=1&autostart=1`, { replace: true })
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not start meeting recording', 'error')
    }
  }, [clearPrompt, navigate, notify])

  useEffect(() => {
    void muesli.meetingDetectionRendererReady?.()

    const unsubscribeShow = muesli.onMeetingDetectionPromptShow?.(handlePromptShow)
    const unsubscribeClear = muesli.onMeetingDetectionPromptClear?.(handlePromptClear)
    const unsubscribeAutoRecord = muesli.onMeetingDetectionAutoRecord?.(handleAutoRecord)
    return () => {
      unsubscribeShow?.()
      unsubscribeClear?.()
      unsubscribeAutoRecord?.()
    }
  }, [handleAutoRecord, handlePromptClear, handlePromptShow])

  const acceptPrompt = useCallback(async () => {
    if (!promptEvent) return
    const key = meetingOccurrenceKey(promptEvent)
    clearPrompt()
    await muesli.meetingDetectionPromptAccept?.(key)
    await createLinkedMeetingNote(promptEvent, navigate, notify, refresh)
  }, [clearPrompt, navigate, notify, promptEvent, refresh])

  const dismissPrompt = useCallback(() => {
    if (!promptEvent) return
    const key = meetingOccurrenceKey(promptEvent)
    clearPrompt()
    void muesli.meetingDetectionPromptDismiss?.(key)
  }, [clearPrompt, promptEvent])

  return {
    promptEvent,
    acceptPrompt,
    dismissPrompt,
  }
}
