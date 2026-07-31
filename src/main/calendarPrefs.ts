import { existsSync, readFileSync, renameSync, rmSync, unlinkSync, writeFileSync, chmodSync } from 'node:fs'
import { join } from 'node:path'
import { DEFAULT_CALENDAR_PREFS, type CalendarPrefs } from '../shared/calendarPrefs'

interface PersistedCalendarPrefs {
  autoRecordDetectedMeetings: boolean
}

const FILE = 'calendar-prefs.json'

function writeFileAtomic(path: string, data: string): void {
  const tempPath = `${path}.tmp-${process.pid}`
  try {
    writeFileSync(tempPath, data, { mode: 0o600 })
    renameSync(tempPath, path)
    chmodSync(path, 0o600)
  } catch (error) {
    try {
      unlinkSync(tempPath)
    } catch {
      // Best-effort cleanup only.
    }
    throw error
  }
}

export class CalendarPrefsStore {
  constructor(private readonly userDataDir: string) {}

  private get path(): string {
    return join(this.userDataDir, FILE)
  }

  load(): CalendarPrefs {
    if (!existsSync(this.path)) return { ...DEFAULT_CALENDAR_PREFS }
    try {
      const raw = JSON.parse(readFileSync(this.path, 'utf8')) as Partial<PersistedCalendarPrefs>
      return {
        autoRecordDetectedMeetings: raw.autoRecordDetectedMeetings === true,
      }
    } catch {
      return { ...DEFAULT_CALENDAR_PREFS }
    }
  }

  save(prefs: CalendarPrefs): void {
    const shape: PersistedCalendarPrefs = {
      autoRecordDetectedMeetings: prefs.autoRecordDetectedMeetings,
    }
    writeFileAtomic(this.path, JSON.stringify(shape))
  }

  clear(): void {
    if (existsSync(this.path)) rmSync(this.path)
  }

  // Test helper so the file format and path remain easy to assert when needed.
  rawFileContents(): string {
    return existsSync(this.path) ? readFileSync(this.path, 'utf8') : ''
  }
}
