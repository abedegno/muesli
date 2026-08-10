import { Pin } from 'lucide-react'
import { muesli } from '@/api'
import { useToast } from '@/components/ui/Toast'
import { NoteListItem } from './NoteListItem'
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger, ContextMenuTrigger } from '@/components/ui/ContextMenu'
import type { Folder, Note } from '../../shared/types'
import { useAgentCapability } from '@/lib/agentCapability'
import { AI_UNAVAILABLE_MESSAGE } from './AgentUnavailableNotice'

export type NoteGroup = { label: string; notes: Note[] }

export function FeedNoteRow({
  note,
  folders,
  refresh,
  onOpen,
}: {
  note: Note
  folders: Folder[]
  refresh: () => void
  onOpen: () => void
}) {
  const { notify } = useToast()
  const pinned = Boolean(note.pinned)
  const agentConfigured = useAgentCapability()

  const run = async (fn: () => Promise<void>) => {
    try {
      await fn()
      refresh()
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      notify(message === 'no default agent configured' ? AI_UNAVAILABLE_MESSAGE : message, 'error')
    }
  }

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <div
          className="group relative w-full text-left"
          draggable
          onDragStart={(e) => e.dataTransfer.setData('text/note-id', note.id)}
        >
          <button
            type="button"
            className="w-full text-left"
            onClick={onOpen}
          >
            <NoteListItem note={note} folders={folders} />
          </button>
          <button
            type="button"
            aria-label={pinned ? 'Unpin note' : 'Pin note'}
            onClick={(e) => {
              e.stopPropagation()
              void run(() => (pinned ? muesli.unpinNote(note.id) : muesli.pinNote(note.id)))
            }}
            className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-muted-foreground opacity-0 transition hover:bg-muted hover:text-foreground group-hover:opacity-100 group-focus-within:opacity-100"
          >
            <Pin size={14} />
          </button>
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem destructive onSelect={() => run(() => muesli.deleteNote(note.id))}>
          Move to Trash
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuSub>
          <ContextMenuSubTrigger>Add to folder</ContextMenuSubTrigger>
          <ContextMenuSubContent>
            {folders.length === 0 ? (
              <ContextMenuItem disabled className="text-muted-foreground">
                No folders yet
              </ContextMenuItem>
            ) : (
              folders.map((f) => (
                <ContextMenuItem
                  key={f.id}
                  onSelect={() => run(() => muesli.addNoteFolder(note.id, f.id))}
                >
                  {f.name}
                </ContextMenuItem>
              ))
            )}
          </ContextMenuSubContent>
        </ContextMenuSub>
        <ContextMenuSeparator />
        <ContextMenuItem
          disabled={agentConfigured === false}
          title={agentConfigured === false ? AI_UNAVAILABLE_MESSAGE : undefined}
          onSelect={() => run(() => muesli.resummarize(note.id))}
          className="data-[disabled]:cursor-not-allowed data-[disabled]:text-muted-foreground"
        >
          Re-run summary
          {agentConfigured === false ? <span className="text-xs">Agent plugin required</span> : null}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}

export function GroupedNoteSections({
  groups,
  folders,
  refresh,
  onOpenNote,
}: {
  groups: NoteGroup[]
  folders: Folder[]
  refresh: () => void
  onOpenNote: (note: Note) => void
}) {
  return (
    <>
      {groups.map((g) => (
        <section key={g.label}>
          <h2 className="px-1 pb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">{g.label}</h2>
          <ul className="flex flex-col gap-1">
            {g.notes.map((n) => (
              <li key={n.id}>
                <FeedNoteRow
                  note={n}
                  folders={folders}
                  refresh={refresh}
                  onOpen={() => onOpenNote(n)}
                />
              </li>
            ))}
          </ul>
        </section>
      ))}
    </>
  )
}
