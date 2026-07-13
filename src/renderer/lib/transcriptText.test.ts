import { describe, expect, it } from 'vitest'
import { transcriptToPlainText } from './transcriptText'

describe('transcriptToPlainText', () => {
  it('renders multiple segments with speakers', () => {
    expect(transcriptToPlainText([
      { start_ms: 0, end_ms: 1000, text: 'Hello', source: 'mixed', speaker: 'Alice' },
      { start_ms: 1000, end_ms: 2000, text: 'Hi there', source: 'mixed', speaker: 'Bob' },
    ])).toBe('Alice: Hello\nBob: Hi there')
  })

  it('renders segments without a speaker as plain text', () => {
    expect(transcriptToPlainText([
      { start_ms: 0, end_ms: 1000, text: 'Hello', source: 'mixed' },
      { start_ms: 1000, end_ms: 2000, text: 'Still here', source: 'mixed', speaker: null },
    ])).toBe('Hello\nStill here')
  })

  it('renders an empty array as an empty string', () => {
    expect(transcriptToPlainText([])).toBe('')
  })
})
