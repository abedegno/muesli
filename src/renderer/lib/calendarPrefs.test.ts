// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { loadCalendarPrefs, saveCalendarPrefs } from './calendarPrefs'

beforeEach(() => localStorage.clear())

describe('loadCalendarPrefs', () => {
  it('returns true only when the stored value is "1"', () => {
    localStorage.setItem('muesli.calendar.autoRecordDetectedMeetings', '1')
    expect(loadCalendarPrefs().autoRecordDetectedMeetings).toBe(true)
  })

  it('returns false when the stored value is "0", missing, or arbitrary', () => {
    localStorage.setItem('muesli.calendar.autoRecordDetectedMeetings', '0')
    expect(loadCalendarPrefs().autoRecordDetectedMeetings).toBe(false)

    localStorage.clear()
    expect(loadCalendarPrefs().autoRecordDetectedMeetings).toBe(false)

    localStorage.setItem('muesli.calendar.autoRecordDetectedMeetings', 'anything-else')
    expect(loadCalendarPrefs().autoRecordDetectedMeetings).toBe(false)
  })

  it('falls back to the default when storage lookup throws', () => {
    const spy = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('storage unavailable')
    })

    expect(loadCalendarPrefs().autoRecordDetectedMeetings).toBe(false)

    spy.mockRestore()
  })
})

describe('saveCalendarPrefs', () => {
  it('writes "1" for true and "0" for false', () => {
    saveCalendarPrefs({ autoRecordDetectedMeetings: true })
    expect(localStorage.getItem('muesli.calendar.autoRecordDetectedMeetings')).toBe('1')

    saveCalendarPrefs({ autoRecordDetectedMeetings: false })
    expect(localStorage.getItem('muesli.calendar.autoRecordDetectedMeetings')).toBe('0')
  })

  it('does not throw when storage write fails', () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('storage unavailable')
    })

    expect(() => saveCalendarPrefs({ autoRecordDetectedMeetings: true })).not.toThrow()

    spy.mockRestore()
  })
})

describe('saveCalendarPrefs / loadCalendarPrefs round-trip', () => {
  it.each([true, false])('preserves %s', (autoRecordDetectedMeetings) => {
    saveCalendarPrefs({ autoRecordDetectedMeetings })
    expect(loadCalendarPrefs().autoRecordDetectedMeetings).toBe(autoRecordDetectedMeetings)
  })
})
