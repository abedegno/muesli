import { describe, it, expect } from 'vitest'
import { fullNoteToMarkdown } from './noteMarkdown'
import type { FullNote } from '../../shared/types'

function makeNote(overrides: Partial<FullNote> = {}): FullNote {
  return {
    note: {
      id: 'n1',
      title: 'Title',
      status: 'ready',
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
      partial_transcript: false,
    },
    body_markdown: '',
    transcript: null,
    summaries: [],
    ...overrides,
  }
}

describe('fullNoteToMarkdown', () => {
  it('renders title, summary, body, and transcript', () => {
    const md = fullNoteToMarkdown(
      makeNote({
        summaries: [
          {
            template_name: 'Meeting Summary',
            status: 'ready',
            sections: [{ heading: 'Key points', content_markdown: '- did a thing' }],
          },
        ],
        body_markdown: 'my own notes',
        transcript: { segments: [{ start_ms: 0, end_ms: 1000, text: 'hello world', source: 'mixed' }] },
      }),
    )
    expect(md).toContain('# Title')
    expect(md).toContain('## Meeting Summary')
    expect(md).toContain('### Key points')
    expect(md).toContain('- did a thing')
    expect(md).toContain('## My notes')
    expect(md).toContain('my own notes')
    expect(md).toContain('## Transcript')
    expect(md).toContain('hello world')
  })

  it('renders just the title for a minimal note', () => {
    const md = fullNoteToMarkdown(makeNote())
    expect(md).toContain('# Title')
    expect(md).not.toContain('## My notes')
    expect(md).not.toContain('## Transcript')
  })

  it('falls back to Untitled when the title is empty', () => {
    const md = fullNoteToMarkdown(makeNote({ note: { ...makeNote().note, title: '' } }))
    expect(md).toContain('# Untitled')
  })

  it('renders a single speaker segment as "**Speaker:** text"', () => {
    const md = fullNoteToMarkdown(
      makeNote({
        transcript: {
          segments: [
            { start_ms: 0, end_ms: 1000, text: 'Hello there', source: 'mixed', speaker: 'Alice' },
          ],
        },
      }),
    )
    expect(md).toContain('**Alice:** Hello there')
  })

  it('collapses consecutive same-speaker segments into one block', () => {
    const md = fullNoteToMarkdown(
      makeNote({
        transcript: {
          segments: [
            { start_ms: 0, end_ms: 500, text: 'First bit.', source: 'mixed', speaker: 'A' },
            { start_ms: 500, end_ms: 1000, text: 'Second bit.', source: 'mixed', speaker: 'A' },
          ],
        },
      }),
    )
    expect(md).toContain('**A:** First bit. Second bit.')
    const occurrences = md.split('**A:**').length - 1
    expect(occurrences).toBe(1)
  })

  it('does not collapse consecutive segments from different speakers', () => {
    const md = fullNoteToMarkdown(
      makeNote({
        transcript: {
          segments: [
            { start_ms: 0, end_ms: 500, text: 'Hi.', source: 'mixed', speaker: 'A' },
            { start_ms: 500, end_ms: 1000, text: 'Hey.', source: 'mixed', speaker: 'B' },
          ],
        },
      }),
    )
    expect(md).toContain('**A:** Hi.')
    expect(md).toContain('**B:** Hey.')
  })

  it('renders a no-speaker segment interleaved among speaker segments as its own plain block', () => {
    const md = fullNoteToMarkdown(
      makeNote({
        transcript: {
          segments: [
            { start_ms: 0, end_ms: 500, text: 'Hi.', source: 'mixed', speaker: 'A' },
            { start_ms: 500, end_ms: 1000, text: 'Ambient noise.', source: 'mixed' },
            { start_ms: 1000, end_ms: 1500, text: 'Hey.', source: 'mixed', speaker: 'B' },
          ],
        },
      }),
    )
    expect(md).toContain('**A:** Hi.')
    expect(md).toContain('Ambient noise.')
    expect(md).not.toContain('**Ambient')
    expect(md).toContain('**B:** Hey.')
  })
})

// ---------------------------------------------------------------------------
// fullNoteToPlainText
// ---------------------------------------------------------------------------
import { fullNoteToPlainText } from './noteMarkdown'

describe('fullNoteToPlainText', () => {
  it('includes title as first line with no Markdown heading marker', () => {
    const txt = fullNoteToPlainText(makeNote())
    const lines = txt.split('\n')
    expect(lines[0]).toBe('Title')
    expect(lines[0]).not.toMatch(/^#/)
  })

  it('falls back to "Untitled" when title is empty', () => {
    const txt = fullNoteToPlainText(makeNote({ note: { ...makeNote().note, title: '' } }))
    expect(txt.startsWith('Untitled')).toBe(true)
  })

  it('renders summary section heading and content without ## markers', () => {
    const txt = fullNoteToPlainText(
      makeNote({
        summaries: [
          {
            template_name: 'Meeting Summary',
            status: 'ready',
            sections: [{ heading: 'Key points', content_markdown: '## not a heading\n**bold text**' }],
          },
        ],
      }),
    )
    expect(txt).toContain('Key points')
    expect(txt).not.toContain('## Key points')
    // ATX heading markers are stripped from content lines; inline markup is preserved as-is
    expect(txt).toContain('**bold text**')
    expect(txt).toContain('not a heading')
    // The ## prefix on the content line is removed but ** is kept
    expect(txt).not.toContain('## not a heading')
  })

  it('includes body under "My notes" heading when body_markdown is non-empty', () => {
    const txt = fullNoteToPlainText(makeNote({ body_markdown: 'my own notes' }))
    expect(txt).toContain('My notes')
    expect(txt).toContain('my own notes')
    // No ## prefix on the heading
    expect(txt).not.toContain('## My notes')
  })

  it('omits "My notes" section when body_markdown is empty or whitespace', () => {
    const txt = fullNoteToPlainText(makeNote({ body_markdown: '   ' }))
    expect(txt).not.toContain('My notes')
  })

  it('renders speaker turn as "Speaker: text"', () => {
    const txt = fullNoteToPlainText(
      makeNote({
        transcript: {
          segments: [
            { start_ms: 0, end_ms: 1000, text: 'Hello there', source: 'mixed', speaker: 'Alice' },
          ],
        },
      }),
    )
    expect(txt).toContain('Alice: Hello there')
  })

  it('renders turn without speaker as just the text (no colon prefix)', () => {
    const txt = fullNoteToPlainText(
      makeNote({
        transcript: {
          segments: [
            { start_ms: 0, end_ms: 1000, text: 'Hello there', source: 'mixed' },
          ],
        },
      }),
    )
    expect(txt).toContain('Hello there')
    expect(txt).not.toContain(': Hello there')
  })

  it('places a blank line between consecutive transcript segments', () => {
    const txt = fullNoteToPlainText(
      makeNote({
        transcript: {
          segments: [
            { start_ms: 0, end_ms: 500, text: 'First', source: 'mixed', speaker: 'A' },
            { start_ms: 500, end_ms: 1000, text: 'Second', source: 'mixed', speaker: 'B' },
          ],
        },
      }),
    )
    const lines = txt.split('\n')
    const firstIdx = lines.indexOf('A: First')
    const secondIdx = lines.indexOf('B: Second')
    expect(firstIdx).toBeGreaterThan(-1)
    expect(secondIdx).toBeGreaterThan(-1)
    // There must be at least one blank line between them
    expect(lines.slice(firstIdx + 1, secondIdx).some((l) => l === '')).toBe(true)
  })

  it('does not include timecodes (start_ms / end_ms) in output', () => {
    const txt = fullNoteToPlainText(
      makeNote({
        transcript: {
          segments: [
            { start_ms: 12345, end_ms: 67890, text: 'Some words', source: 'mixed' },
          ],
        },
      }),
    )
    expect(txt).not.toContain('12345')
    expect(txt).not.toContain('67890')
    expect(txt).not.toContain('start_ms')
    expect(txt).not.toContain('end_ms')
  })

  it('omits transcript section entirely when transcript is null', () => {
    const txt = fullNoteToPlainText(makeNote({ transcript: null }))
    expect(txt).not.toContain('Transcript')
  })

  it('omits transcript section entirely when segments array is empty', () => {
    const txt = fullNoteToPlainText(makeNote({ transcript: { segments: [] } }))
    expect(txt).not.toContain('Transcript')
  })

  it('preserves underscores and asterisks in body content', () => {
    const txt = fullNoteToPlainText(
      makeNote({ body_markdown: 'meeting_notes_v2 and *important* item' }),
    )
    expect(txt).toContain('meeting_notes_v2')
    expect(txt).toContain('*important*')
  })
})
