import type { CalendarEvent } from '../../shared/types'

export type MeetingDetectionAction = 'noop' | 'prompt' | 'auto-record'

export interface MeetingDetectionLoopDecision {
  action: MeetingDetectionAction
  event: CalendarEvent | null
  occurrenceKey: string | null
}

export interface MeetingDetectionLoopInput {
  detectedEvent: CalendarEvent | null
  dismissedOccurrences: ReadonlySet<string>
  surfacedOccurrences: ReadonlySet<string>
  alreadyRecording: boolean
  autoRecordDetectedMeetings: boolean
  /** Optional future gate for the auto-record branch. Defaults to allowed. */
  allowAutoRecord?: boolean
}

// Stable per-occurrence identity so a recurring event's next occurrence is treated as fresh.
export function meetingOccurrenceKey(event: Pick<CalendarEvent, 'id' | 'starts_at'>): string {
  return `${event.id}::${event.starts_at}`
}

export function decideMeetingDetectionAction(input: MeetingDetectionLoopInput): MeetingDetectionLoopDecision {
  const { detectedEvent, dismissedOccurrences, surfacedOccurrences, alreadyRecording, autoRecordDetectedMeetings, allowAutoRecord = true } = input

  if (!detectedEvent) {
    return { action: 'noop', event: null, occurrenceKey: null }
  }

  const occurrenceKey = meetingOccurrenceKey(detectedEvent)
  if (alreadyRecording) return { action: 'noop', event: detectedEvent, occurrenceKey }
  if (dismissedOccurrences.has(occurrenceKey)) return { action: 'noop', event: detectedEvent, occurrenceKey }
  if (surfacedOccurrences.has(occurrenceKey)) return { action: 'noop', event: detectedEvent, occurrenceKey }

  if (autoRecordDetectedMeetings && allowAutoRecord) {
    return { action: 'auto-record', event: detectedEvent, occurrenceKey }
  }

  return { action: 'prompt', event: detectedEvent, occurrenceKey }
}
