import { describe, it, expect } from 'vitest'
import { normalizeTitle, suggestRecurring } from './recurring'
import type { Note, SmartList } from '../../shared/types'

const note = (title: string): Note => ({ id: title, title, status: 'ready', created_at: '', updated_at: '', tags: [], partial_transcript: false })

describe('normalizeTitle', () => {
  it('lowercases, trims, strips trailing date/number/(n) tokens', () => {
    expect(normalizeTitle('Weekly Standup Jun 13')).toBe('weekly standup')
    expect(normalizeTitle('Weekly Standup 2026-06-20')).toBe('weekly standup')
    expect(normalizeTitle('Standup (3)')).toBe('standup')
    expect(normalizeTitle('Standup #14')).toBe('standup')
    expect(normalizeTitle('  Retro  ')).toBe('retro')
  })
})

describe('suggestRecurring', () => {
  it('suggests stems with >=3 notes, excludes already-saved title-contains lists', () => {
    const notes: Note[] = [
      note('Weekly Standup Jun 13'), note('Weekly Standup Jun 20'), note('Weekly Standup Jun 27'),
      note('1:1 with Alice'), note('1:1 with Bob'),
    ]
    expect(suggestRecurring(notes, [])).toEqual([{ stem: 'weekly standup', count: 3 }])

    const existing: SmartList[] = [{ id: 'l', name: 'Standups', created_at: '', rule: { op: 'and', children: [{ field: 'title', operator: 'contains', value: 'Weekly Standup' }] } }]
    expect(suggestRecurring(notes, existing)).toEqual([])
  })

  it('a narrow list value does NOT suppress a broader stem', () => {
    const notes = [note('Weekly Standup Jun 13'), note('Weekly Standup Jun 20'), note('Weekly Standup Jun 27')]
    const narrow: SmartList[] = [{ id: 'l', name: 'All standups', created_at: '', rule: { op: 'and', children: [{ field: 'title', operator: 'contains', value: 'standup' }] } }]
    // value "standup" does NOT contain the stem "weekly standup" → still suggested
    expect(suggestRecurring(notes, narrow)).toEqual([{ stem: 'weekly standup', count: 3 }])
  })

  it('a covering list value (== or superstring of the stem) suppresses it', () => {
    const notes = [note('Weekly Standup Jun 13'), note('Weekly Standup Jun 20'), note('Weekly Standup Jun 27')]
    const covering: SmartList[] = [{ id: 'l', name: 'WS', created_at: '', rule: { op: 'and', children: [{ field: 'title', operator: 'contains', value: 'Weekly Standup Series' }] } }]
    expect(suggestRecurring(notes, covering)).toEqual([])
  })
})
