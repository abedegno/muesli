import { describe, it, expect } from 'vitest'
import { countFolder, descendantIds } from './folders'
import type { Note, Folder } from '../../shared/types'

const note = (over: Partial<Note>): Note => ({
  id: '1', title: '', status: 'ready', created_at: '', updated_at: '', folder_ids: [], partial_transcript: false, ...over,
} as Note)
const folder: Folder = { id: 'f1', name: 'Clients', created_at: '' }

describe('countFolder', () => {
  it('counts notes whose folder_ids include the folder', () => {
    const notes = [note({ id: 'a', folder_ids: ['f1'] }), note({ id: 'b', folder_ids: ['f2'] }), note({ id: 'c' })]
    expect(countFolder(notes, [folder], folder)).toBe(1)
  })
  it('treats a missing folder_ids as empty', () => {
    expect(countFolder([note({ folder_ids: undefined })], [folder], folder)).toBe(0)
  })
  it('includes notes in descendant folders (recursive)', () => {
    const folders: Folder[] = [
      { id: 'parent', name: 'Parent', created_at: '' },
      { id: 'child', name: 'Child', parent_id: 'parent', created_at: '' },
    ]
    const notes = [
      note({ id: 'a', folder_ids: ['parent'] }),
      note({ id: 'b', folder_ids: ['child'] }),
      note({ id: 'c', folder_ids: ['other'] }),
    ]
    expect(countFolder(notes, folders, folders[0])).toBe(2)
  })
  it('counts a note in both parent and child only once', () => {
    const folders: Folder[] = [
      { id: 'parent', name: 'Parent', created_at: '' },
      { id: 'child', name: 'Child', parent_id: 'parent', created_at: '' },
    ]
    const notes = [note({ id: 'a', folder_ids: ['parent', 'child'] })]
    expect(countFolder(notes, folders, folders[0])).toBe(1)
  })
})

describe('descendantIds', () => {
  const folders: Folder[] = [
    { id: 'root', name: 'Root', created_at: '' },
    { id: 'child', name: 'Child', parent_id: 'root', created_at: '' },
    { id: 'grand', name: 'Grandchild', parent_id: 'child', created_at: '' },
    { id: 'other', name: 'Unrelated', created_at: '' },
  ]
  it('returns the root and its whole subtree (child + grandchild)', () => {
    const ids = descendantIds(folders, 'root')
    expect(ids).toEqual(new Set(['root', 'child', 'grand']))
  })
  it('excludes unrelated folders', () => {
    expect(descendantIds(folders, 'root').has('other')).toBe(false)
  })
  it('returns just the id for a leaf', () => {
    expect(descendantIds(folders, 'grand')).toEqual(new Set(['grand']))
  })
})
