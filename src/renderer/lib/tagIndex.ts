import type { Note } from '../../shared/types'

export interface TagCount {
  // The server-side tag row id. Client-derived tags (from note.tags strings) have
  // no id, so tagIndex emits ''; the server tag list (GET /api/tags) carries the
  // real id, which the sidebar needs to rename a tag.
  id: string
  name: string
  count: number
}

// tagIndex derives the unique tags + counts across notes, sorted case-insensitively.
// Names are compared case-insensitively but displayed as first seen.
export function tagIndex(notes: Note[]): TagCount[] {
  const counts = new Map<string, { id: string; name: string; count: number }>()
  for (const n of notes) {
    for (const t of n.tags ?? []) {
      const key = t.toLowerCase()
      const existing = counts.get(key)
      if (existing) existing.count++
      else counts.set(key, { id: '', name: t, count: 1 })
    }
  }
  return [...counts.values()].sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()))
}
