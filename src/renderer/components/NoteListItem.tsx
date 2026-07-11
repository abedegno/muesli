import { Badge } from '@/components/ui/Badge'
import { statusLabel, statusTone } from '@/lib/status'
import { monogram, type MonogramTone } from '@/lib/monogram'
import { formatTime } from '@/lib/datetime'
import { Pin } from 'lucide-react'
import type { Note, Folder } from '../../shared/types'

const TILE: Record<MonogramTone, string> = {
  teal: 'bg-primary/15 text-primary',
  blue: 'bg-blue-500/15 text-blue-600 dark:text-blue-300',
  violet: 'bg-violet-500/15 text-violet-600 dark:text-violet-300',
  amber: 'bg-amber-500/15 text-amber-600 dark:text-amber-300',
  rose: 'bg-rose-500/15 text-rose-600 dark:text-rose-300',
  slate: 'bg-slate-500/15 text-slate-600 dark:text-slate-300',
}

export function NoteListItem({ note, folders }: { note: Note; folders?: Folder[] }) {
  const { initial, tone } = monogram(note)
  const pinned = Boolean(note.pinned)

  const folderNames: string[] =
    note.folder_ids && note.folder_ids.length > 0 && folders && folders.length > 0
      ? note.folder_ids
          .map((id) => folders.find((f) => f.id === id)?.name)
          .filter((name): name is string => name !== undefined)
      : []

  return (
    <div className="flex items-start gap-3 rounded-[var(--radius)] px-3 py-2 pr-10 hover:bg-muted">
      <div className={`mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-sm font-semibold ${TILE[tone]}`}>
        {initial}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{note.title || 'Untitled'}</div>
        {note.snippet && <div className="truncate text-sm text-muted-foreground">{note.snippet}</div>}
        {note.tags && note.tags.length > 0 && (
          <div className="mt-1 flex flex-wrap gap-1">
            {note.tags.map((tag, index) => (
              <Badge key={`${tag}-${index}`} tone="neutral">{tag}</Badge>
            ))}
          </div>
        )}
        {folderNames.length > 0 && (
          <div className="mt-1 flex flex-wrap gap-1">
            {folderNames.map((name, index) => (
              <Badge key={`folder-${name}-${index}`} tone="neutral">{name}</Badge>
            ))}
          </div>
        )}
      </div>
      <div className="flex shrink-0 flex-col items-end gap-1">
        {pinned && (
          <span title="Pinned" aria-label="Pinned" className="text-muted-foreground">
            <Pin size={12} />
          </span>
        )}
        <Badge tone={statusTone(note.status)}>{statusLabel(note.status)}</Badge>
        {note.created_at && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground">{formatTime(note.created_at)}</span>
        )}
      </div>
    </div>
  )
}
