import { Fragment, useState } from 'react'
import { useNavigate, useOutletContext } from 'react-router-dom'
import { Pin } from 'lucide-react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from '@/components/ui/ContextMenu'
import { useToast } from '@/components/ui/Toast'
import { cn } from '@/lib/cn'
import { EmptyState } from './EmptyState'
import { NoteListItem } from './NoteListItem'
import { Skeleton } from '@/components/ui/Skeleton'
import { groupNotesByDate } from '@/lib/datetime'
import type { Folder, Note, SearchMatch } from '../../shared/types'
import type { ActiveView } from './shell/AppLayout'

interface Ctx {
  notes: Note[]
  semanticNotes?: Note[]
  semanticMatches?: Record<string, SearchMatch[]>
  searchQuery?: string
  refresh: () => void
  folders: Folder[]
  heading: string
  isFiltered: boolean
  view: ActiveView
  loaded: boolean
  searching: boolean
  onReorderNote: (folderId: string, movedNoteId: string, afterId: string | null) => void
}

function FeedNoteRow({
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

  const run = async (fn: () => Promise<void>) => {
    try {
      await fn()
      refresh()
    } catch (err) {
      notify(err instanceof Error ? err.message : String(err), 'error')
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
        <ContextMenuItem onSelect={() => run(() => muesli.resummarize(note.id))}>
          Re-run summary
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
}

function ReorderGap({
  folderId,
  afterId,
  label,
  onReorderNote,
}: {
  folderId: string
  afterId: string | null
  label: string
  onReorderNote: (folderId: string, movedNoteId: string, afterId: string | null) => void
}) {
  const [dragOver, setDragOver] = useState(false)
  return (
    <li
      aria-label={label}
      className={cn('h-1.5 w-full', dragOver && 'bg-primary')}
      onDragOver={(e) => {
        if (e.dataTransfer.types.includes('text/note-id')) {
          e.preventDefault()
          setDragOver(true)
        }
      }}
      onDragLeave={() => setDragOver(false)}
      onDrop={(e) => {
        setDragOver(false)
        const movedNoteId = e.dataTransfer.getData('text/note-id')
        if (!movedNoteId || movedNoteId === afterId) return
        onReorderNote(folderId, movedNoteId, afterId)
      }}
    />
  )
}

const MATCH_TYPE_LABEL: Record<SearchMatch['match_type'], string> = {
  title: 'Title',
  transcript: 'Transcript',
  summary: 'Summary',
}

// One row per match on a semantic-search hit: a small type badge (Title /
// Transcript / Summary) plus the server-provided context snippet, if any.
// Clicking a transcript hit navigates to the note AND jumps to the exact
// segment (via the `segment` query param, resolved by NoteScreen/NoteView);
// title/summary hits just open the note.
function MatchRow({ noteId, match, onNavigate }: { noteId: string; match: SearchMatch; onNavigate: (path: string) => void }) {
  const target =
    match.match_type === 'transcript' && match.segment_id
      ? `/notes/${noteId}?segment=${encodeURIComponent(match.segment_id)}`
      : `/notes/${noteId}`
  return (
    <button
      type="button"
      aria-label={`${MATCH_TYPE_LABEL[match.match_type]} match${match.snippet ? `: ${match.snippet}` : ''}`}
      data-testid={`match-${match.match_type}-${noteId}`}
      onClick={() => onNavigate(target)}
      className="ml-3 flex w-[calc(100%-0.75rem)] items-center gap-2 rounded-[var(--radius)] px-2 py-1 text-left text-xs text-muted-foreground hover:bg-muted"
    >
      <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide">
        {MATCH_TYPE_LABEL[match.match_type]}
      </span>
      {match.snippet && <span className="truncate">{match.snippet}</span>}
    </button>
  )
}

/** First-run onboarding hint shown only when no notes exist yet (USE04). */
function OnboardingHint() {
  return (
    <ol
      className="mx-auto mt-2 max-w-sm space-y-2 text-left text-sm text-muted-foreground"
      data-testid="onboarding-hint"
    >
      <li>
        <span className="font-medium text-foreground">1. Click &ldquo;New meeting&rdquo;</span>
        {' — '}open the desktop client, hit <em>New meeting</em>, and recording starts immediately.
      </li>
      <li>
        <span className="font-medium text-foreground">2. Record your meeting</span>
        {' — '}the desktop client captures your mic and system audio and uploads it when you stop.
      </li>
      <li>
        <span className="font-medium text-foreground">3. Get your transcript &amp; AI summary</span>
        {' — '}the server automatically transcribes the audio and produces an AI-generated summary.
      </li>
      <li>
        <span className="font-medium text-foreground">4. Your note appears here</span>
        {' — '}once processing is complete, the finished note lands in this list, ready to read and search.
      </li>
    </ol>
  )
}

export function NotesListScreen() {
  const { notes, semanticNotes = [], semanticMatches = {}, folders, heading, view, loaded, refresh, searching, searchQuery = '', onReorderNote } = useOutletContext<Ctx>()
  const navigate = useNavigate()
  const showFolderOrdering = view.type === 'folder' && searchQuery.trim() === ''

  if (!loaded) {
    return (
      <div role="status" aria-label="Loading notes" className="mx-auto max-w-3xl p-6" data-testid="notes-loading">
        <div className="mb-4 h-7 w-40"><Skeleton className="h-full w-full" /></div>
        <div className="flex flex-col gap-2">
          {Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}
        </div>
      </div>
    )
  }

  const groups = notes.length > 0 ? groupNotesByDate(notes, new Date()) : []
  return (
    <div className="mx-auto max-w-3xl p-6">
      <h1 className="mb-4 font-serif text-xl font-semibold">{heading}</h1>
      {searching && <Skeleton className="h-1 w-full mb-3" data-testid="search-loading" />}
      {notes.length === 0 && semanticNotes.length === 0 ? (
        searchQuery.trim() !== '' ? (
          <EmptyState title="No matching notes" hint={`No notes matched "${searchQuery}". Try broader keywords or a different view.`} />
        ) : view.type === 'folder' ? (
          <EmptyState title="This folder is empty" hint="Drag a note here or start a new meeting." />
        ) : view.type === 'list' ? (
          <EmptyState title="No notes match this smart list" />
        ) : view.type === 'tag' ? (
          <EmptyState title="No notes with this tag" />
        ) : (
          <>
            <EmptyState
              title="No notes yet"
              hint="Start your first meeting and your notes will appear here."
              action={<Button onClick={() => navigate('/new')}>New meeting</Button>}
            />
            <OnboardingHint />
          </>
        )
      ) : (
        <div className="flex flex-col gap-4">
          {view.type === 'folder' ? (
            <ul className="flex flex-col gap-1">
              {showFolderOrdering && (
                <ReorderGap
                  folderId={view.id}
                  afterId={null}
                  label="reorder gap first"
                  onReorderNote={onReorderNote}
                />
              )}
              {notes.map((n) => (
                <Fragment key={n.id}>
                  <li>
                    <FeedNoteRow
                      note={n}
                      folders={folders}
                      refresh={refresh}
                      onOpen={() => navigate(`/notes/${n.id}`)}
                    />
                  </li>
                  {showFolderOrdering && (
                    <ReorderGap
                      folderId={view.id}
                      afterId={n.id}
                      label={`reorder gap after ${n.title || 'Untitled'}`}
                      onReorderNote={onReorderNote}
                    />
                  )}
                </Fragment>
              ))}
            </ul>
          ) : (
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
                          onOpen={() => navigate(`/notes/${n.id}`)}
                        />
                      </li>
                    ))}
                  </ul>
                </section>
              ))}
            </>
          )}
          {semanticNotes.length > 0 && (
            <div className="mt-6">
              <h2 className="px-1 pb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground">Similar results</h2>
              <ul className="flex flex-col gap-1">
                {semanticNotes.map((n) => (
                  <li key={n.id} className="flex flex-col gap-1">
                    <FeedNoteRow note={n} folders={folders} refresh={refresh} onOpen={() => navigate(`/notes/${n.id}`)} />
                    {(semanticMatches[n.id] ?? []).map((m, i) => (
                      <MatchRow key={i} noteId={n.id} match={m} onNavigate={navigate} />
                    ))}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
