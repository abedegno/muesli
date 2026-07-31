import { Fragment, useEffect, useState } from 'react'
import { useNavigate, useOutletContext } from 'react-router-dom'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { cn } from '@/lib/cn'
import { EmptyState } from './EmptyState'
import { FeedNoteRow, GroupedNoteSections } from './NoteFeed'
import { Skeleton } from '@/components/ui/Skeleton'
import { groupNotesByDate } from '@/lib/datetime'
import type { Folder, Note, SearchMatch } from '../../shared/types'
import type { ActiveView } from './shell/AppLayout'

interface Ctx {
  notes: Note[]
  allNotes?: Note[]
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
function OnboardingHint({ isEmbeddedMode }: { isEmbeddedMode: boolean }) {
  return (
    <ol
      className="mx-auto mt-2 max-w-sm space-y-2 text-left text-sm text-muted-foreground"
      data-testid="onboarding-hint"
    >
      <li>
        <span className="font-medium text-foreground">1. Click &ldquo;New meeting&rdquo;</span>
        {' — '}start a new meeting here to begin recording your microphone and system audio.
      </li>
      <li>
        <span className="font-medium text-foreground">2. Let the app process it</span>
        {' — '}
        {isEmbeddedMode
          ? 'the recording stays on this device, where processing happens locally on this device and creates the summary.'
          : 'the recording goes to your connected server, which transcribes it and creates the summary.'}
      </li>
      <li>
        <span className="font-medium text-foreground">3. Review the finished note</span>
        {' — '}when processing finishes, the note appears in this list, ready to open, search, and refine.
      </li>
    </ol>
  )
}

export function NotesListScreen() {
  const { notes, semanticNotes = [], semanticMatches = {}, folders, heading, view, loaded, refresh, searching, searchQuery = '', onReorderNote } = useOutletContext<Ctx>()
  const navigate = useNavigate()
  const [isEmbeddedMode, setIsEmbeddedMode] = useState(true)
  const showFolderOrdering = view.type === 'folder' && searchQuery.trim() === ''

  useEffect(() => {
    let cancelled = false
    void Promise.resolve(muesli.getManualServer?.())
      .then((manualServer) => {
        if (!cancelled) setIsEmbeddedMode(!manualServer)
      })
      .catch(() => {
        if (!cancelled) setIsEmbeddedMode(true)
      })

    return () => {
      cancelled = true
    }
  }, [])

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
            <OnboardingHint isEmbeddedMode={isEmbeddedMode} />
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
          <GroupedNoteSections
            groups={groups}
            folders={folders}
            refresh={refresh}
            onOpenNote={(note) => navigate(`/notes/${note.id}`)}
          />
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
