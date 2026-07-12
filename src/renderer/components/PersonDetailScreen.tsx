import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { EmptyState } from './EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import type { Note, PersonWithCompany } from '../../shared/types'

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function formatNoteDate(note: Note): string {
  const iso = note.started_at ?? note.created_at
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

function ErrorBlock({ message, subject }: { message: string; subject: string }) {
  return (
    <div className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
      <p className="font-medium">Could not load {subject}</p>
      <p className="mt-1 break-words">{message}</p>
    </div>
  )
}

function NoteRow({ note }: { note: Note }) {
  return (
    <li>
      <Link
        to={`/notes/${note.id}`}
        className="block rounded-[var(--radius)] border border-border px-3 py-2 transition-colors hover:bg-muted"
      >
        <p className="truncate text-sm font-medium">{note.title || 'Untitled note'}</p>
        <p className="truncate text-xs text-muted-foreground">{formatNoteDate(note)}</p>
      </Link>
    </li>
  )
}

export function PersonDetailScreen() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [person, setPerson] = useState<PersonWithCompany | null>(null)
  const [notes, setNotes] = useState<Note[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setPerson(null)
    setNotes(null)
    setError(null)

    void Promise.all([muesli.getPerson(id), muesli.getPersonNotes(id)])
      .then(([loadedPerson, loadedNotes]) => {
        if (cancelled) return
        setPerson(loadedPerson)
        setNotes(loadedNotes)
      })
      .catch((err) => {
        if (cancelled) return
        setError(errorMessage(err))
        setPerson(null)
        setNotes([])
      })

    return () => {
      cancelled = true
    }
  }, [id])

  const loading = person === null && notes === null && error === null
  const isEmpty = notes !== null && notes.length === 0 && error === null

  return (
    <div className="mx-auto max-w-3xl p-6">
      <div className="mb-4 flex items-center gap-2">
        <Button variant="ghost" size="sm" onClick={() => navigate('/people')} className="px-2">
          <ArrowLeft size={14} />
          Back to People
        </Button>
      </div>

      {loading ? (
        <div className="flex flex-col gap-2">
          <div className="mb-2 h-7 w-52"><Skeleton className="h-full w-full" /></div>
          <div className="h-5 w-72"><Skeleton className="h-full w-full" /></div>
          <div className="mt-2 flex flex-col gap-2">
            {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}
          </div>
        </div>
      ) : error ? (
        <ErrorBlock message={error} subject="person" />
      ) : person ? (
        <div className="space-y-6">
          <section className="space-y-2">
            <h1 className="font-serif text-xl font-semibold">{person.display_name || 'Untitled person'}</h1>
            <p className="text-sm text-muted-foreground">{person.primary_email}</p>
            <p className="text-sm text-muted-foreground">{person.company?.name ?? '—'}</p>
          </section>

          {isEmpty ? (
            <EmptyState title="No notes yet" hint="This person has not appeared in any notes or meetings." />
          ) : (
            <section>
              <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Notes</h2>
              <ul className="flex flex-col gap-2">
                {notes?.map((note) => <NoteRow key={note.id} note={note} />)}
              </ul>
            </section>
          )}
        </div>
      ) : null}
    </div>
  )
}
