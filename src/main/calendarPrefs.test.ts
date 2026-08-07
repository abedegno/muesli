import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { DEFAULT_CALENDAR_PREFS } from '../shared/calendarPrefs'
import { CalendarPrefsStore } from './calendarPrefs'

describe('CalendarPrefsStore', () => {
  let dir: string

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), 'muesli-calendar-prefs-'))
  })

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true })
  })

  it('returns defaults when the preferences file is absent', () => {
    const store = new CalendarPrefsStore(dir)
    expect(store.load()).toEqual(DEFAULT_CALENDAR_PREFS)
    expect(store.rawFileContents()).toBe('')
  })

  it('round-trips preferences through the persisted file', () => {
    const store = new CalendarPrefsStore(dir)
    store.save({ autoRecordDetectedMeetings: true })

    expect(new CalendarPrefsStore(dir).load()).toEqual({ autoRecordDetectedMeetings: true })
    expect(JSON.parse(store.rawFileContents())).toEqual({ autoRecordDetectedMeetings: true })
  })

  it('does not throw and returns defaults for corrupt or partial JSON', () => {
    writeFileSync(join(dir, 'calendar-prefs.json'), '{ "autoRecordDetectedMeetings": tru')
    const store = new CalendarPrefsStore(dir)

    expect(() => store.load()).not.toThrow()
    expect(store.load()).toEqual(DEFAULT_CALENDAR_PREFS)
  })

  it('clears saved preferences', () => {
    const store = new CalendarPrefsStore(dir)
    store.save({ autoRecordDetectedMeetings: true })
    store.clear()

    expect(store.rawFileContents()).toBe('')
    expect(store.load()).toEqual(DEFAULT_CALENDAR_PREFS)
  })
})
