import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { EmptyState } from './EmptyState'
import { MonogramAvatar } from './MonogramAvatar'
import { Skeleton } from '@/components/ui/Skeleton'
import type { CompanyWithPeople, Note, Person } from '../../shared/types'

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function ErrorBlock({ message, subject }: { message: string; subject: string }) {
  return (
    <div className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
      <p className="font-medium">Could not load {subject}</p>
      <p className="mt-1 break-words">{message}</p>
    </div>
  )
}

function formatCount(count: number): string {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(count)
}

function formatHours(hours: number): string {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 1 }).format(hours)
}

function formatNoteDate(note: Note): string {
  const iso = note.started_at ?? note.created_at
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

function ActivityStat({
  label,
  value,
}: {
  label: string
  value: string
}) {
  return (
    <div className="rounded-[var(--radius)] border border-border bg-card px-3 py-2">
      <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-1 text-sm font-semibold text-foreground">{value}</p>
    </div>
  )
}

function ActivityRollup({
  notes,
}: {
  notes: Note[]
}) {
  const meetingCount = notes.length
  const totalHours = notes.reduce((sum, note) => {
    if (!note.started_at || !note.ended_at) return sum
    const startedAt = new Date(note.started_at).getTime()
    const endedAt = new Date(note.ended_at).getTime()
    return sum + (endedAt - startedAt) / (1000 * 60 * 60)
  }, 0)

  const lastSeen = notes.reduce<Note | null>((latest, note) => {
    if (!latest) return note
    const candidateTime = new Date(note.started_at ?? note.created_at).getTime()
    const latestTime = new Date(latest.started_at ?? latest.created_at).getTime()
    return candidateTime > latestTime ? note : latest
  }, null)

  return (
    <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
      <ActivityStat label="Meetings" value={formatCount(meetingCount)} />
      <ActivityStat label="Hours" value={formatHours(totalHours)} />
      <ActivityStat label="Last seen" value={lastSeen ? formatNoteDate(lastSeen) : 'Never'} />
    </div>
  )
}

function PersonRow({ person }: { person: Person }) {
  return (
    <li>
      <Link
        to={`/people/${person.id}`}
        className="block rounded-[var(--radius)] border border-border px-3 py-2 transition-colors hover:bg-muted"
      >
        <p className="truncate text-sm font-medium">{person.display_name || 'Untitled person'}</p>
        <p className="truncate text-xs text-muted-foreground">{person.primary_email}</p>
      </Link>
    </li>
  )
}

export function CompanyDetailScreen() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [company, setCompany] = useState<CompanyWithPeople | null>(null)
  const [activityNotes, setActivityNotes] = useState<Note[] | null>(null)
  const [activityError, setActivityError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setCompany(null)
    setActivityNotes(null)
    setActivityError(null)
    setError(null)

    void muesli.getCompany(id)
      .then((loadedCompany) => {
        if (cancelled) return
        setCompany(loadedCompany)
      })
      .catch((err) => {
        if (cancelled) return
        setError(errorMessage(err))
        setCompany(null)
      })

    return () => {
      cancelled = true
    }
  }, [id])

  useEffect(() => {
    let cancelled = false

    if (!company) return
    if (company.people.length === 0) {
      setActivityNotes(null)
      setActivityError(null)
      return
    }

    setActivityNotes(null)
    setActivityError(null)

    void Promise.all(company.people.map((person) => muesli.getPersonNotes(person.id)))
      .then((notesByPerson) => {
        if (cancelled) return
        const byId = new Map<string, Note>()
        for (const notes of notesByPerson) {
          for (const note of notes) {
            byId.set(note.id, note)
          }
        }
        setActivityNotes(Array.from(byId.values()))
      })
      .catch((err) => {
        if (cancelled) return
        setActivityError(errorMessage(err))
        setActivityNotes(null)
      })

    return () => {
      cancelled = true
    }
  }, [company])

  const loading = company === null && error === null
  const isEmpty = company !== null && company.people.length === 0 && error === null

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
          <div className="h-5 w-64"><Skeleton className="h-full w-full" /></div>
          <div className="mt-2 flex flex-col gap-2">
            {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}
          </div>
        </div>
      ) : error ? (
        <ErrorBlock message={error} subject="company" />
      ) : company ? (
        <div className="space-y-6">
          <section className="flex items-start gap-3">
            <MonogramAvatar id={company.id} label={company.name || company.domain} />
            <div className="min-w-0 flex-1 space-y-2">
              <h1 className="font-serif text-xl font-semibold">{company.name || 'Untitled company'}</h1>
              <p className="truncate text-sm text-muted-foreground">{company.domain}</p>
            </div>
          </section>

          {activityError ? (
            <ErrorBlock message={activityError} subject="company activity" />
          ) : null}

          {activityNotes !== null ? <ActivityRollup notes={activityNotes} /> : null}

          {isEmpty ? (
            <EmptyState title="No people yet" hint="No people are associated with this company." />
          ) : (
            <section>
              <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">People</h2>
              <ul className="flex flex-col gap-2">
                {company.people.map((person) => <PersonRow key={person.id} person={person} />)}
              </ul>
            </section>
          )}
        </div>
      ) : null}
    </div>
  )
}
