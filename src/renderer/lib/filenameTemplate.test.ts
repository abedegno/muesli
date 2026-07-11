import { describe, it, expect } from 'vitest'
import { renderFilenameTemplate, validateFilenameTemplate } from './filenameTemplate'

describe('renderFilenameTemplate', () => {
  it('replaces {title} and {date} placeholders', () => {
    expect(
      renderFilenameTemplate('{title} - {date}', { date: '2024-01-15', title: 'My Meeting' }),
    ).toBe('My Meeting - 2024-01-15')
  })

  it('replaces multiple occurrences of a placeholder', () => {
    expect(
      renderFilenameTemplate('{date} {title} {date}', { date: '2024-01-15', title: 'X' }),
    ).toBe('2024-01-15 X 2024-01-15')
  })

  it('leaves the template unchanged when no placeholders are present', () => {
    expect(renderFilenameTemplate('my-file', { date: '2024-01-15', title: 'X' })).toBe('my-file')
  })

  it('replaces invalid filename characters in the rendered output with _', () => {
    // A title containing a slash and colon
    expect(
      renderFilenameTemplate('{title} - {date}', { date: '2024-01-15', title: 'foo/bar:baz' }),
    ).toBe('foo_bar_baz - 2024-01-15')
  })

  it('replaces all invalid chars: / \\ : * ? " < > |', () => {
    const bad = '/\\:*?"<>|'
    const result = renderFilenameTemplate(bad, { date: '2024-01-15', title: 'X' })
    expect(result).toBe('_________')
  })
})

describe('validateFilenameTemplate', () => {
  it('returns no errors for a valid template', () => {
    expect(validateFilenameTemplate('{title} - {date}')).toEqual([])
  })

  it('returns an error for an unknown placeholder', () => {
    const errors = validateFilenameTemplate('{title} - {foo}')
    expect(errors).toHaveLength(1)
    expect(errors[0]).toMatch(/\{foo\}/)
  })

  it('returns an error for multiple unknown placeholders (one error each)', () => {
    const errors = validateFilenameTemplate('{foo} {bar}')
    expect(errors.length).toBeGreaterThanOrEqual(2)
    expect(errors.some((e) => e.includes('{foo}'))).toBe(true)
    expect(errors.some((e) => e.includes('{bar}'))).toBe(true)
  })

  it('returns an error for an empty template (only spaces)', () => {
    const errors = validateFilenameTemplate('   ')
    expect(errors.length).toBeGreaterThan(0)
  })

  it('returns no error for a template that sanitizes to underscores (valid non-empty filename)', () => {
    // '/' sanitizes to '_', which is a valid (non-empty) filename — not an error
    const errors = validateFilenameTemplate('/')
    expect(errors).toEqual([])
  })

  it('returns no errors for a template with only known placeholders', () => {
    expect(validateFilenameTemplate('{date}')).toEqual([])
    expect(validateFilenameTemplate('{title}')).toEqual([])
  })
})
