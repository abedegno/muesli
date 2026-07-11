import { describe, it, expect } from 'vitest'
import { tagIndex } from './tagIndex'
import type { Note } from '../../shared/types'

const note = (id: string, tags: string[]): Note => ({
  id, title: id, status: 'ready', created_at: '', updated_at: '', tags, partial_transcript: false,
})

describe('tagIndex', () => {
  it('counts tags across notes and sorts by name (case-insensitive)', () => {
    const idx = tagIndex([note('1', ['1on1', 'hiring']), note('2', ['1on1']), note('3', [])])
    // Client-derived tags have no server id, so tagIndex emits id: ''.
    expect(idx).toEqual([{ id: '', name: '1on1', count: 2 }, { id: '', name: 'hiring', count: 1 }])
  })
  it('handles notes with no tags field', () => {
    expect(tagIndex([{ id: '1', title: 'x', status: 'ready', created_at: '', updated_at: '', partial_transcript: false }])).toEqual([])
  })
})
