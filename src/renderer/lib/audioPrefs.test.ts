// @vitest-environment jsdom
import { describe, it, expect, beforeEach } from 'vitest'
import { loadAudioPrefs, saveAudioPrefs } from './audioPrefs'

beforeEach(() => localStorage.clear())

describe('loadAudioPrefs', () => {
  it('returns defaults when nothing is stored', () => {
    const prefs = loadAudioPrefs()
    expect(prefs.deviceId).toBeUndefined()
    expect(prefs.gain).toBe(1.0)
  })
})

describe('saveAudioPrefs / loadAudioPrefs round-trip', () => {
  it('writes and reads back deviceId and gain', () => {
    saveAudioPrefs({ deviceId: 'mic-abc-123', gain: 1.5 })
    expect(localStorage.getItem('muesli.audio.deviceId')).toBe('mic-abc-123')
    expect(localStorage.getItem('muesli.audio.gain')).toBe('1.5')
    const prefs = loadAudioPrefs()
    expect(prefs.deviceId).toBe('mic-abc-123')
    expect(prefs.gain).toBe(1.5)
  })

  it('parses gain as a float', () => {
    localStorage.setItem('muesli.audio.gain', '0.75')
    const prefs = loadAudioPrefs()
    expect(prefs.gain).toBeCloseTo(0.75)
  })

  it('removes deviceId from storage when undefined', () => {
    localStorage.setItem('muesli.audio.deviceId', 'old-device')
    saveAudioPrefs({ deviceId: undefined, gain: 1.0 })
    expect(localStorage.getItem('muesli.audio.deviceId')).toBeNull()
    const prefs = loadAudioPrefs()
    expect(prefs.deviceId).toBeUndefined()
  })

  it('falls back to default gain when stored value is not a number', () => {
    localStorage.setItem('muesli.audio.gain', 'bogus')
    const prefs = loadAudioPrefs()
    expect(prefs.gain).toBe(1.0)
  })
})
