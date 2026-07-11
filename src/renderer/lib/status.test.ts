import { describe, it, expect } from 'vitest'
import { statusLabel, statusTone, isProcessing } from './status'
import type { NoteStatus } from '../../shared/types'

describe('status helpers', () => {
  describe('statusLabel', () => {
    const cases: [NoteStatus, string][] = [
      ['recording',   'Recording'],
      ['uploaded',    'Uploaded'],
      ['transcribing','Transcribing'],
      ['summarizing', 'Summarizing'],
      ['ready',       'Ready'],
      ['failed',      'Failed'],
    ]
    it.each(cases)('statusLabel(%s) === %s', (status, expected) => {
      expect(statusLabel(status)).toBe(expected)
    })
  })

  describe('statusTone', () => {
    const cases: [NoteStatus, 'neutral' | 'primary' | 'accent' | 'destructive'][] = [
      ['recording',   'neutral'],
      ['uploaded',    'accent'],
      ['transcribing','accent'],
      ['summarizing', 'accent'],
      ['ready',       'primary'],
      ['failed',      'destructive'],
    ]
    it.each(cases)('statusTone(%s) === %s', (status, expected) => {
      expect(statusTone(status)).toBe(expected)
    })
  })

  describe('isProcessing', () => {
    const cases: [NoteStatus, boolean][] = [
      ['recording',   false],
      ['uploaded',    true],
      ['transcribing',true],
      ['summarizing', true],
      ['ready',       false],
      ['failed',      false],
    ]
    it.each(cases)('isProcessing(%s) === %s', (status, expected) => {
      expect(isProcessing(status)).toBe(expected)
    })
  })
})
