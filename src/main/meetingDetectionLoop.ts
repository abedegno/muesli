import type { BrowserWindow } from 'electron'
import { detectActiveMeeting } from '../shared/meetingDetect'
import {
  decideMeetingDetectionAction,
  MEETING_DETECTION_LOOP_INTERVAL_MS,
  MEETING_DETECTION_WINDOW_BACK_MS,
  MEETING_DETECTION_WINDOW_FORWARD_MS,
} from '../shared/meetingDetectionLoop'
import type { CalendarPrefs } from '../shared/calendarPrefs'
import type { CalendarEvent, Note } from '../shared/types'

export interface MeetingDetectionRendererPayload {
  event: CalendarEvent
  occurrenceKey: string
}

export interface MeetingDetectionManagerDeps {
  getCalendarEvents(from: string, to: string): Promise<CalendarEvent[]>
  listNotes(): Promise<Note[]>
  getCalendarPrefs(): CalendarPrefs
  createAutoRecordNote(event: CalendarEvent): Promise<string>
  ensureWindow(): Promise<BrowserWindow | null> | BrowserWindow | null
  hasWindow(): boolean
  focusWindow(): void
  sendPromptShow(payload: MeetingDetectionRendererPayload): void
  sendPromptClear(payload: { occurrenceKey: string }): void
  sendAutoRecord(payload: { noteId: string }): void
  showNotification(payload: MeetingDetectionRendererPayload, onClick: () => void): void
  log?(message: string, err?: unknown): void
}

export class MeetingDetectionManager {
  private readonly surfacedOccurrences = new Set<string>()
  private readonly dismissedOccurrences = new Set<string>()
  private activePrompt: MeetingDetectionRendererPayload | null = null
  private visiblePromptKey: string | null = null
  private pendingAutoRecord: { noteId: string } | null = null
  private notifiedOccurrences = new Set<string>()
  private inFlight = false
  private intervalId: ReturnType<typeof setInterval> | null = null
  private rendererReady = false

  constructor(private readonly deps: MeetingDetectionManagerDeps) {}

  start(): void {
    if (this.intervalId !== null) return
    void this.tick()
    this.intervalId = setInterval(() => {
      void this.tick()
    }, MEETING_DETECTION_LOOP_INTERVAL_MS)
  }

  stop(): void {
    if (this.intervalId === null) return
    clearInterval(this.intervalId)
    this.intervalId = null
  }

  windowClosed(): void {
    this.rendererReady = false
    this.visiblePromptKey = null
  }

  async rendererReadyForWindow(): Promise<void> {
    this.rendererReady = true
    await this.flushPending()
  }

  acceptPrompt(occurrenceKey: string): void {
    if (this.activePrompt?.occurrenceKey !== occurrenceKey) return
    this.surfacedOccurrences.add(occurrenceKey)
    this.deps.sendPromptClear({ occurrenceKey })
    this.activePrompt = null
    this.visiblePromptKey = null
  }

  dismissPrompt(occurrenceKey: string): void {
    if (this.activePrompt?.occurrenceKey !== occurrenceKey) return
    this.dismissedOccurrences.add(occurrenceKey)
    this.activePrompt = null
    this.visiblePromptKey = null
    this.deps.sendPromptClear({ occurrenceKey })
  }

  private clearVisiblePrompt(): void {
    if (!this.activePrompt) return
    if (this.visiblePromptKey === this.activePrompt.occurrenceKey) {
      this.deps.sendPromptClear({ occurrenceKey: this.activePrompt.occurrenceKey })
    }
    this.activePrompt = null
    this.visiblePromptKey = null
  }

  private async flushPending(): Promise<void> {
    if (!this.rendererReady) return
    if (this.pendingAutoRecord) {
      const payload = this.pendingAutoRecord
      this.pendingAutoRecord = null
      this.deps.sendAutoRecord(payload)
      return
    }
    if (this.activePrompt && this.visiblePromptKey !== this.activePrompt.occurrenceKey) {
      this.deps.sendPromptShow(this.activePrompt)
      this.visiblePromptKey = this.activePrompt.occurrenceKey
    }
  }

  private async tick(): Promise<void> {
    if (this.inFlight) return
    this.inFlight = true
    try {
      const now = new Date()
      const from = new Date(now.getTime() - MEETING_DETECTION_WINDOW_BACK_MS).toISOString()
      const to = new Date(now.getTime() + MEETING_DETECTION_WINDOW_FORWARD_MS).toISOString()

      let events: CalendarEvent[]
      try {
        events = await this.deps.getCalendarEvents(from, to)
      } catch (err) {
        this.deps.log?.('meeting detection calendar fetch failed', err)
        this.clearVisiblePrompt()
        return
      }

      if (!events.length) {
        this.clearVisiblePrompt()
        return
      }

      let notes: Note[]
      try {
        notes = await this.deps.listNotes()
      } catch (err) {
        this.deps.log?.('meeting detection note fetch failed', err)
        return
      }

      const prefs = this.deps.getCalendarPrefs()
      const active = detectActiveMeeting(events, now, prefs)
      const decision = decideMeetingDetectionAction({
        detectedEvent: active,
        dismissedOccurrences: this.dismissedOccurrences,
        surfacedOccurrences: this.surfacedOccurrences,
        // Recording is unambiguous capture activity. A draft only represents
        // capture intent for the calendar event it was created for; unrelated
        // drafts must not suppress detection indefinitely.
        alreadyRecording: notes.some(
          (n) => n.status === 'recording' || (n.status === 'draft' && n.event_id === active?.id),
        ),
        autoRecordDetectedMeetings: prefs.autoRecordDetectedMeetings,
      })

      if (decision.action === 'auto-record' && decision.event && decision.occurrenceKey) {
        if (!this.deps.hasWindow()) {
          void this.deps.ensureWindow()
        }
        const noteId = await this.deps.createAutoRecordNote(decision.event)
        this.surfacedOccurrences.add(decision.occurrenceKey)
        this.activePrompt = null
        this.visiblePromptKey = null
        this.pendingAutoRecord = { noteId }
        await this.flushPending()
        return
      }

      if (decision.action === 'prompt' && decision.event && decision.occurrenceKey) {
        const payload = { event: decision.event, occurrenceKey: decision.occurrenceKey }
        if (this.activePrompt?.occurrenceKey === payload.occurrenceKey) {
          if (this.rendererReady && this.deps.hasWindow() && this.visiblePromptKey !== payload.occurrenceKey) {
            this.deps.sendPromptShow(payload)
            this.visiblePromptKey = payload.occurrenceKey
          }
          return
        }

        this.clearVisiblePrompt()
        this.activePrompt = payload

        if (this.deps.hasWindow() && this.rendererReady) {
          this.deps.sendPromptShow(payload)
          this.visiblePromptKey = payload.occurrenceKey
          return
        }

        if (!this.deps.hasWindow() && !this.notifiedOccurrences.has(payload.occurrenceKey)) {
          this.notifiedOccurrences.add(payload.occurrenceKey)
          this.surfacedOccurrences.add(payload.occurrenceKey)
          this.activePrompt = null
          this.deps.showNotification(payload, () => {
            this.deps.focusWindow()
          })
        }
        return
      }

      this.clearVisiblePrompt()
    } finally {
      this.inFlight = false
    }
  }
}

export {
  meetingOccurrenceKey,
  MEETING_DETECTION_LOOP_INTERVAL_MS,
  MEETING_DETECTION_WINDOW_BACK_MS,
  MEETING_DETECTION_WINDOW_FORWARD_MS,
} from '../shared/meetingDetectionLoop'
