import type { Note, Folder } from '../../shared/types'

// countFolder counts notes in a folder OR any of its descendant folders. A note
// filed in both a parent and a child is counted once (via `some`).
export function countFolder(notes: Note[], folders: Folder[], folder: Folder): number {
  const ids = descendantIds(folders, folder.id)
  return notes.reduce((acc, n) => acc + ((n.folder_ids ?? []).some((id) => ids.has(id)) ? 1 : 0), 0)
}

// descendantIds returns the set of ids in the subtree rooted at rootId (incl. rootId).
export function descendantIds(folders: Folder[], rootId: string): Set<string> {
  const out = new Set<string>([rootId])
  let added = true
  while (added) {
    added = false
    for (const f of folders) {
      if (f.parent_id && out.has(f.parent_id) && !out.has(f.id)) { out.add(f.id); added = true }
    }
  }
  return out
}
