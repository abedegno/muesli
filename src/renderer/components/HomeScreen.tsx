import { useMemo, type ReactNode } from 'react'
import { useNavigate, useOutletContext } from 'react-router-dom'
import { Button } from '@/components/ui/Button'
import { EmptyState } from './EmptyState'
import { GroupedNoteSections, type NoteGroup } from './NoteFeed'
import { UpcomingEventsPanel } from './UpcomingEventsPanel'
import { groupNotesByDate } from '@/lib/datetime'
import type { Folder, Note } from '../../shared/types'

interface Ctx {
  allNotes: Note[]
  folders: Folder[]
  refresh: () => void
  loaded: boolean
}

const RECENT_NOTE_LIMIT = 5

function recentNotes(notes: Note[], limit: number): Note[] {
  const time = (iso: string): number => {
    const ms = new Date(iso).getTime()
    return Number.isNaN(ms) ? 0 : ms
  }
  return [...notes].sort((a, b) => time(b.created_at) - time(a.created_at)).slice(0, limit)
}

export function HomeScreen() {
  const { allNotes = [], folders, refresh, loaded } = useOutletContext<Ctx>()
  const navigate = useNavigate()
  const now = useMemo(() => new Date(), [])

  if (!loaded) {
    return (
      <div className="mx-auto max-w-3xl p-6">
        <div className="mb-8 h-7 w-24 rounded bg-muted/60" />
        <div className="flex flex-col gap-8">
          <div className="space-y-3 rounded-[var(--radius)] border border-border p-4">
            <div className="h-5 w-28 rounded bg-muted/60" />
            <div className="space-y-2">
              <div className="h-12 rounded bg-muted/60" />
              <div className="h-12 rounded bg-muted/60" />
              <div className="h-12 rounded bg-muted/60" />
            </div>
          </div>
          <div className="space-y-3 rounded-[var(--radius)] border border-border p-4">
            <div className="h-5 w-28 rounded bg-muted/60" />
            <div className="space-y-2">
              <div className="h-12 rounded bg-muted/60" />
              <div className="h-12 rounded bg-muted/60" />
              <div className="h-12 rounded bg-muted/60" />
            </div>
          </div>
        </div>
      </div>
    )
  }

  const recentGroups: NoteGroup[] = groupNotesByDate(recentNotes(allNotes, RECENT_NOTE_LIMIT), now)
  const sections: Array<{ id: string; render: () => ReactNode }> = [
    {
      id: 'upcoming',
      render: () => (
        <section>
          <h2 className="mb-3 font-serif text-xl font-semibold">Upcoming</h2>
          <UpcomingEventsPanel />
        </section>
      ),
    },
    {
      id: 'recent-notes',
      render: () => (
        <section>
          <h2 className="mb-3 font-serif text-xl font-semibold">Recent notes</h2>
          {recentGroups.length === 0 ? (
            <EmptyState
              title="No recent notes yet"
              hint="Start your first meeting and it will appear here."
              action={<Button onClick={() => navigate('/new')}>New meeting</Button>}
            />
          ) : (
            <GroupedNoteSections
              groups={recentGroups}
              folders={folders}
              refresh={refresh}
              onOpenNote={(note) => navigate(`/notes/${note.id}`)}
            />
          )}
        </section>
      ),
    },
  ]

  return (
    <div className="mx-auto max-w-3xl p-6">
      <h1 className="mb-4 font-serif text-xl font-semibold">Home</h1>
      <ol className="flex list-none flex-col gap-8 p-0">
        {sections.map((section) => (
          <li key={section.id}>
            {section.render()}
          </li>
        ))}
      </ol>
    </div>
  )
}
