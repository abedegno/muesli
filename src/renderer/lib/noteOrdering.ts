import type { Note } from '../../shared/types'

// Stable partition that keeps the existing intra-group order while moving pinned
// notes ahead of unpinned ones. Callers can layer any other ordering they need
// before invoking this helper.
export function sortNotesPinnedFirst<T extends Pick<Note, 'pinned'>>(notes: T[]): T[] {
  const pinned: T[] = []
  const unpinned: T[] = []
  for (const note of notes) {
    if (note.pinned) pinned.push(note)
    else unpinned.push(note)
  }
  return [...pinned, ...unpinned]
}
