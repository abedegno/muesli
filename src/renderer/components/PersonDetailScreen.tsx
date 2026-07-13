import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ArrowLeft } from 'lucide-react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { EmptyState } from './EmptyState'
import { MonogramAvatar } from './MonogramAvatar'
import { Input } from '@/components/ui/Input'
import { Skeleton } from '@/components/ui/Skeleton'
import type { CompanyWithCount, Note, PersonWithCompany } from '../../shared/types'

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function formatNoteDate(note: Note): string {
  const iso = note.started_at ?? note.created_at
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

function formatCount(count: number): string {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(count)
}

function formatHours(hours: number): string {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 1 }).format(hours)
}

function ErrorBlock({ message, subject }: { message: string; subject: string }) {
  return (
    <div className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
      <p className="font-medium">Could not load {subject}</p>
      <p className="mt-1 break-words">{message}</p>
    </div>
  )
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
  const [companies, setCompanies] = useState<CompanyWithCount[] | null>(null)
  const [people, setPeople] = useState<PersonWithCompany[] | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [optionsError, setOptionsError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [editOpen, setEditOpen] = useState(false)
  const [draftDisplayName, setDraftDisplayName] = useState('')
  const [draftCompanyId, setDraftCompanyId] = useState('')
  const [mergeTarget, setMergeTarget] = useState('')
  const [saving, setSaving] = useState(false)
  const [merging, setMerging] = useState(false)
  const [deleting, setDeleting] = useState(false)

  useEffect(() => {
    let cancelled = false
    setPerson(null)
    setNotes(null)
    setLoadError(null)
    setActionError(null)
    setEditOpen(false)

    void Promise.all([muesli.getPerson(id), muesli.getPersonNotes(id)])
      .then(([loadedPerson, loadedNotes]) => {
        if (cancelled) return
        setPerson(loadedPerson)
        setNotes(loadedNotes)
      })
      .catch((err) => {
        if (cancelled) return
        setLoadError(errorMessage(err))
        setPerson(null)
        setNotes([])
      })

    return () => {
      cancelled = true
    }
  }, [id])

  useEffect(() => {
    let cancelled = false
    setCompanies(null)
    setPeople(null)
    setOptionsError(null)

    void Promise.all([muesli.listCompanies(), muesli.listPeople()])
      .then(([loadedCompanies, loadedPeople]) => {
        if (cancelled) return
        setCompanies(loadedCompanies)
        setPeople(loadedPeople)
      })
      .catch((err) => {
        if (cancelled) return
        setOptionsError(errorMessage(err))
        setCompanies([])
        setPeople([])
      })

    return () => {
      cancelled = true
    }
  }, [id])

  useEffect(() => {
    if (!person || !people) return
    const nextTarget = people.find((candidate) => candidate.id !== person.id)?.id ?? ''
    setMergeTarget((current) => (current && current !== person.id ? current : nextTarget))
  }, [person, people])

  const loading = person === null && notes === null && loadError === null
  const isEmpty = notes !== null && notes.length === 0 && loadError === null
  const isEditReady = companies !== null
  const isMergeReady = people !== null

  async function handleSaveEdit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!person) return
    setActionError(null)
    setSaving(true)
    try {
      const updated = await muesli.updatePerson(person.id, {
        displayName: draftDisplayName,
        companyId: draftCompanyId ? draftCompanyId : null,
      })
      setPerson(updated)
      setEditOpen(false)
    } catch (err) {
      setActionError(errorMessage(err))
    } finally {
      setSaving(false)
    }
  }

  async function handleMergePeople() {
    if (!person || !mergeTarget) return
    setActionError(null)
    setMerging(true)
    try {
      const merged = await muesli.mergePeople(person.id, mergeTarget)
      navigate(`/people/${merged.id}`)
    } catch (err) {
      setActionError(errorMessage(err))
    } finally {
      setMerging(false)
    }
  }

  async function handleDeletePerson() {
    if (!person) return
    const ok = window.confirm(`Delete ${person.display_name || person.primary_email}?`)
    if (!ok) return

    setActionError(null)
    setDeleting(true)
    try {
      await muesli.deletePerson(person.id)
      navigate('/people')
    } catch (err) {
      setActionError(errorMessage(err))
    } finally {
      setDeleting(false)
    }
  }

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
      ) : loadError ? (
        <ErrorBlock message={loadError} subject="person" />
      ) : person ? (
        <div className="space-y-6">
          <section className="space-y-4">
            <div className="flex items-start gap-3">
              <MonogramAvatar id={person.id} label={person.display_name || person.primary_email} />
              <div className="min-w-0 flex-1">
                <h1 className="font-serif text-xl font-semibold">{person.display_name || 'Untitled person'}</h1>
                <p className="truncate text-sm text-muted-foreground">{person.primary_email}</p>
                {person.company ? (
                  <Link to={`/companies/${person.company.id}`} className="truncate text-sm text-muted-foreground transition-colors hover:text-foreground">
                    {person.company.name || person.company.domain}
                  </Link>
                ) : (
                  <p className="truncate text-sm text-muted-foreground">—</p>
                )}
              </div>
            </div>

            {notes !== null ? <ActivityRollup notes={notes} /> : null}

            {optionsError ? (
              <div className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
                <p className="font-medium">Could not load editing options</p>
                <p className="mt-1 break-words">{optionsError}</p>
              </div>
            ) : null}

            {actionError ? (
              <div className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">
                <p className="font-medium">Action failed</p>
                <p className="mt-1 break-words">{actionError}</p>
              </div>
            ) : null}

            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                onClick={() => {
                  setDraftDisplayName(person.display_name)
                  setDraftCompanyId(person.company?.id ?? '')
                  setEditOpen((open) => !open)
                  setActionError(null)
                }}
              >
                {editOpen ? 'Close edit' : 'Edit'}
              </Button>
              <Button
                type="button"
                variant="ghost"
                onClick={handleDeletePerson}
                disabled={deleting}
                className="text-destructive hover:text-destructive"
              >
                {deleting ? 'Deleting...' : 'Delete'}
              </Button>
            </div>

            {editOpen ? (
              <form onSubmit={handleSaveEdit} className="space-y-3 rounded-[var(--radius)] border border-border bg-muted/30 p-4">
                <div className="space-y-1">
                  <label htmlFor="person-display-name" className="block text-xs font-medium uppercase tracking-wide text-muted-foreground">
                    Display name
                  </label>
                  <Input
                    id="person-display-name"
                    value={draftDisplayName}
                    onChange={(e) => setDraftDisplayName(e.target.value)}
                    placeholder="Untitled person"
                  />
                </div>

                <div className="space-y-1">
                  <label htmlFor="person-company" className="block text-xs font-medium uppercase tracking-wide text-muted-foreground">
                    Company
                  </label>
                  <select
                    id="person-company"
                    className="w-full rounded-[var(--radius)] border border-border bg-background px-3 py-2 text-sm"
                    value={draftCompanyId}
                    onChange={(e) => setDraftCompanyId(e.target.value)}
                    disabled={!isEditReady}
                  >
                    <option value="">No company</option>
                    {companies?.map((company) => (
                      <option key={company.id} value={company.id}>
                        {company.name || company.domain}
                      </option>
                    ))}
                  </select>
                  {!isEditReady ? <p className="text-xs text-muted-foreground">Loading companies...</p> : null}
                </div>

                <div className="flex items-center gap-2">
                  <Button type="submit" disabled={saving || !isEditReady}>
                    {saving ? 'Saving...' : 'Save changes'}
                  </Button>
                  <Button type="button" variant="ghost" onClick={() => setEditOpen(false)} disabled={saving}>
                    Cancel
                  </Button>
                </div>
              </form>
            ) : null}

            <div className="space-y-1 rounded-[var(--radius)] border border-border bg-muted/20 p-4">
              <label htmlFor="person-merge-target" className="block text-xs font-medium uppercase tracking-wide text-muted-foreground">
                Merge into
              </label>
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                <select
                  id="person-merge-target"
                  className="w-full rounded-[var(--radius)] border border-border bg-background px-3 py-2 text-sm"
                  value={mergeTarget}
                  onChange={(e) => setMergeTarget(e.target.value)}
                  disabled={!isMergeReady || merging}
                >
                  <option value="">Choose a person</option>
                  {people?.filter((candidate) => candidate.id !== person.id).map((candidate) => (
                    <option key={candidate.id} value={candidate.id}>
                      {candidate.display_name || candidate.primary_email}
                    </option>
                  ))}
                </select>
                <Button type="button" variant="ghost" onClick={handleMergePeople} disabled={merging || !isMergeReady || !mergeTarget}>
                  {merging ? 'Merging...' : 'Merge'}
                </Button>
              </div>
              {!isMergeReady ? <p className="text-xs text-muted-foreground">Loading people...</p> : null}
            </div>
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
