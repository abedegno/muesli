import { describe, it, expect } from 'vitest'
import { sortNotesPinnedFirst } from './noteOrdering'

describe('sortNotesPinnedFirst', () => {
  it('keeps pinned notes ahead of unpinned notes while preserving the existing order within each group', () => {
    const out = sortNotesPinnedFirst([
      { id: 'a', pinned: false },
      { id: 'b', pinned: true },
      { id: 'c', pinned: true },
      { id: 'd', pinned: false },
    ])

    expect(out.map((n) => n.id)).toEqual(['b', 'c', 'a', 'd'])
  })

  it('returns a new array', () => {
    const input = [{ id: 'a', pinned: true }]
    const out = sortNotesPinnedFirst(input)
    expect(out).not.toBe(input)
  })
})
