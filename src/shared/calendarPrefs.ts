export interface CalendarPrefs {
  autoRecordDetectedMeetings: boolean
}

export const AUTO_RECORD_KEY = 'muesli.calendar.autoRecordDetectedMeetings'

export const DEFAULT_CALENDAR_PREFS: CalendarPrefs = { autoRecordDetectedMeetings: false }
