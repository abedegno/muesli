// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { APP_LANGUAGES, catalogues, translate } from './localization'
import {
  formatDate,
  formatNumber,
  getDisplayLanguage,
  setDisplayLanguage,
  setRegion,
  uiText,
  subscribeLocale,
} from './browserLocale'
afterEach(() => localStorage.clear())
describe('translated catalogues', () => {
  it.each(APP_LANGUAGES)(
    '%s contains all messages with the same named placeholders',
    (language) => {
      expect(Object.keys(catalogues[language]).sort()).toEqual(Object.keys(catalogues.en).sort())
      for (const [source, value] of Object.entries(catalogues[language])) {
        expect(value.trim(), source).not.toBe('')
        const placeholders = (s: string) =>
          [...s.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort()
        expect(placeholders(value), source).toEqual(placeholders(source))
      }
    }
  )
  it('changes controls without translating user content and preserves interpolation', () => {
    setDisplayLanguage('fr')
    expect(uiText('New meeting')).toBe('Nouvelle réunion')
    expect(uiText('My private meeting title')).toBe('My private meeting title')
    expect(translate('es', 'Help: {label}', { label: 'Mi servicio' })).toBe('Ayuda: Mi servicio')
    expect(getDisplayLanguage()).toBe('fr')
  })
  it('keeps regional formatting independent and notifies mounted views', () => {
    let changes = 0
    const unsubscribe = subscribeLocale(() => changes++)
    setDisplayLanguage('de')
    setRegion('en-GB')
    expect(formatNumber(1234.5)).toBe('1,234.5')
    expect(
      formatDate(new Date('2026-12-31T12:00:00Z'), {
        day: '2-digit',
        month: '2-digit',
        year: 'numeric',
        timeZone: 'UTC',
      })
    ).toBe('31/12/2026')
    expect(uiText('New meeting')).toBe('Neues Meeting')
    expect(changes).toBe(2)
    unsubscribe()
    setRegion('de-DE')
    expect(changes).toBe(2)
    expect(formatNumber(1234.5)).toBe('1.234,5')
  })
  it('rejects unsupported selections and falls back for unknown source messages', () => {
    expect(() => setDisplayLanguage('invalid')).toThrow()
    expect(() => setRegion('invalid_locale')).toThrow()
    expect(translate('fr', 'not a catalogue entry')).toBe('not a catalogue entry')
  })
})
