// calendarPrefs — persists small calendar-related client prefs to localStorage
// with safe defaults when storage is unavailable.
import { AUTO_RECORD_KEY, DEFAULT_CALENDAR_PREFS, type CalendarPrefs } from '../../shared/calendarPrefs'

/** Read persisted calendar prefs; falls back to defaults on any storage error. */
export function loadCalendarPrefs(): CalendarPrefs {
  try {
    return {
      autoRecordDetectedMeetings: localStorage.getItem(AUTO_RECORD_KEY) === '1',
    }
  } catch {
    return { ...DEFAULT_CALENDAR_PREFS }
  }
}

/** Persist calendar prefs; failures (storage unavailable) are swallowed. */
export function saveCalendarPrefs(prefs: CalendarPrefs): void {
  try {
    localStorage.setItem(AUTO_RECORD_KEY, prefs.autoRecordDetectedMeetings ? '1' : '0')
  } catch {
    /* storage unavailable — keep in-memory value */
  }
  void window.muesli?.setCalendarPrefs?.(prefs)
}
