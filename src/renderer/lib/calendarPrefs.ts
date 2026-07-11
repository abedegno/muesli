// calendarPrefs — persists small calendar-related client prefs to localStorage
// with safe defaults when storage is unavailable.

export interface CalendarPrefs {
  autoRecordDetectedMeetings: boolean
}

const AUTO_RECORD_KEY = 'muesli.calendar.autoRecordDetectedMeetings'

const DEFAULTS: CalendarPrefs = { autoRecordDetectedMeetings: false }

/** Read persisted calendar prefs; falls back to defaults on any storage error. */
export function loadCalendarPrefs(): CalendarPrefs {
  try {
    return {
      autoRecordDetectedMeetings: localStorage.getItem(AUTO_RECORD_KEY) === '1',
    }
  } catch {
    return { ...DEFAULTS }
  }
}

/** Persist calendar prefs; failures (storage unavailable) are swallowed. */
export function saveCalendarPrefs(prefs: CalendarPrefs): void {
  try {
    localStorage.setItem(AUTO_RECORD_KEY, prefs.autoRecordDetectedMeetings ? '1' : '0')
  } catch {
    /* storage unavailable — keep in-memory value */
  }
}
