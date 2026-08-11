import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { muesli } from '@/api'
import { EmptyState } from './EmptyState'
import { MonogramAvatar } from './MonogramAvatar'
import { Skeleton } from '@/components/ui/Skeleton'
import type { ActionItem, Note, PersonWithCompany } from '../../shared/types'
import { useAgentCapability } from '@/lib/agentCapability'
import { AgentUnavailableNotice } from './AgentUnavailableNotice'

type StatusFilter = 'open' | 'done' | 'all'

const ALL_FILTER_VALUE = 'all'
const UNASSIGNED = 'unassigned'

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message
  return String(err)
}

function compareByText(a: string, b: string): number {
  return a.localeCompare(b, undefined, { sensitivity: 'base' })
}

function formatDateLabel(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

function noteTitle(note: Note | undefined): string {
  return note?.title?.trim() || 'Untitled note'
}

function noteDate(note: Note | undefined): string {
  return note?.started_at ?? note?.created_at ?? ''
}

function personLabel(person: PersonWithCompany | undefined): string {
  return person?.display_name?.trim() || person?.primary_email || 'Untitled person'
}

function noteGroupLabel(note: Note | undefined): string {
  return noteTitle(note)
}

function ActionItemRow({
  item,
  owner,
}: {
  item: ActionItem
  owner?: PersonWithCompany
}) {
  const done = item.status === 'done'
  const ownerName = personLabel(owner)

  return (
    <li>
      <Link
        to={`/notes/${item.note_id}`}
        className={[
          'flex items-start justify-between gap-3 rounded-[var(--radius)] border border-border px-3 py-2 transition-colors hover:bg-muted',
          done ? 'bg-muted/40' : 'bg-background',
        ].join(' ')}
      >
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <p className={[
              'truncate text-sm font-medium',
              done ? 'text-muted-foreground line-through' : 'text-foreground',
            ].join(' ')}>
              {item.text || 'Untitled action item'}
            </p>
            <span
              className={[
                'shrink-0 rounded-full px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide',
                done ? 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' : 'bg-primary/10 text-primary',
              ].join(' ')}
            >
              {done ? 'Done' : 'Open'}
            </span>
          </div>
          {item.due_hint ? (
            <p className="mt-1 text-xs text-muted-foreground">Due: {item.due_hint}</p>
          ) : null}
          {owner ? (
            <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
              <MonogramAvatar id={owner.id} label={ownerName} className="h-5 w-5 text-[10px]" />
              <span className="truncate">{ownerName}</span>
            </div>
          ) : null}
        </div>
        <span className="shrink-0 text-xs text-muted-foreground">Open note</span>
      </Link>
    </li>
  )
}

function NoteGroup({
  noteId,
  note,
  items,
  peopleById,
}: {
  noteId: string
  note?: Note
  items: ActionItem[]
  peopleById: Map<string, PersonWithCompany>
}) {
  const title = noteTitle(note)
  const dateLabel = note ? formatDateLabel(noteDate(note)) : ''

  return (
    <section className="rounded-[var(--radius)] border border-border bg-background/70 p-4">
      <div className="mb-3 flex items-start justify-between gap-3">
        <div className="min-w-0">
          <Link to={`/notes/${noteId}`} className="block truncate text-sm font-semibold hover:text-foreground">
            {title}
          </Link>
          <p className="truncate text-xs text-muted-foreground">{noteId}</p>
        </div>
        <div className="shrink-0 text-right text-xs text-muted-foreground">
          <p>{items.length} item{items.length === 1 ? '' : 's'}</p>
          {dateLabel ? <p>{dateLabel}</p> : null}
        </div>
      </div>
      <ul className="flex flex-col gap-2">
        {items.map((item) => <ActionItemRow key={item.id} item={item} owner={item.owner_person_id ? peopleById.get(item.owner_person_id) : undefined} />)}
      </ul>
    </section>
  )
}

export function ActionItemsScreen() {
  const agentConfigured = useAgentCapability()
  const [notesById, setNotesById] = useState<Record<string, Note> | null>(null)
  const [people, setPeople] = useState<PersonWithCompany[] | null>(null)
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('open')
  const [ownerFilter, setOwnerFilter] = useState<string>(ALL_FILTER_VALUE)
  const [meetingFilter, setMeetingFilter] = useState<string>(ALL_FILTER_VALUE)
  const [actionItems, setActionItems] = useState<ActionItem[] | null>(null)
  const [actionItemsError, setActionItemsError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setNotesById(null)
    setPeople(null)
    setActionItems(null)
    setActionItemsError(null)

    void Promise.all([muesli.listNotes(), muesli.listPeople(), muesli.listActionItems()])
      .then(([notes, peopleList, items]) => {
        if (cancelled) return
        const next: Record<string, Note> = {}
        for (const note of notes ?? []) {
          next[note.id] = note
        }
        setNotesById(next)
        setPeople(peopleList ?? [])
        setActionItems(items ?? [])
      })
      .catch((err) => {
        if (cancelled) return
        setNotesById({})
        setPeople([])
        setActionItems([])
        setActionItemsError(errorMessage(err))
      })

    return () => {
      cancelled = true
    }
  }, [])

  const peopleById = useMemo(() => {
    const next = new Map<string, PersonWithCompany>()
    for (const person of people ?? []) {
      next.set(person.id, person)
    }
    return next
  }, [people])

  const ownerOptions = useMemo(() => {
    const ids = new Set<string>()
    for (const item of actionItems ?? []) {
      if (item.owner_person_id) ids.add(item.owner_person_id)
    }

    return [...ids]
      .map((id) => peopleById.get(id))
      .filter((person): person is PersonWithCompany => Boolean(person))
      .sort((a, b) => {
        const labelCompare = compareByText(personLabel(a), personLabel(b))
        if (labelCompare !== 0) return labelCompare
        return compareByText(a.primary_email, b.primary_email)
      })
  }, [actionItems, peopleById])

  const meetingOptions = useMemo(() => {
    const ids = new Set<string>()
    for (const item of actionItems ?? []) {
      ids.add(item.note_id)
    }

    return [...ids]
      .map((id) => ({ id, note: notesById?.[id] }))
      .sort((a, b) => {
        const titleCompare = compareByText(noteGroupLabel(a.note), noteGroupLabel(b.note))
        if (titleCompare !== 0) return titleCompare
        return noteDate(b.note).localeCompare(noteDate(a.note))
      })
  }, [actionItems, notesById])

  const itemsByOwnerAndMeeting = useMemo(() => {
    return (actionItems ?? []).filter((item) => {
      if (ownerFilter === UNASSIGNED) {
        if (item.owner_person_id !== null) return false
      } else if (ownerFilter !== ALL_FILTER_VALUE && item.owner_person_id !== ownerFilter) {
        return false
      }

      if (meetingFilter !== ALL_FILTER_VALUE && item.note_id !== meetingFilter) {
        return false
      }

      return true
    })
  }, [actionItems, meetingFilter, ownerFilter])

  const visibleItems = useMemo(() => {
    if (statusFilter === 'all') return itemsByOwnerAndMeeting
    return itemsByOwnerAndMeeting.filter((item) => item.status === statusFilter)
  }, [itemsByOwnerAndMeeting, statusFilter])

  const statusCounts = useMemo(() => {
    return itemsByOwnerAndMeeting.reduce(
      (acc, item) => {
        if (item.status === 'done') acc.done += 1
        else acc.open += 1
        return acc
      },
      { open: 0, done: 0 },
    )
  }, [itemsByOwnerAndMeeting])

  const groups = useMemo(() => {
    const byNote = new Map<string, ActionItem[]>()
    for (const item of visibleItems) {
      const current = byNote.get(item.note_id)
      if (current) current.push(item)
      else byNote.set(item.note_id, [item])
    }

    return [...byNote.entries()]
      .map(([noteId, items]) => ({
        noteId,
        note: notesById?.[noteId],
        items: [...items].sort((a, b) => b.created_at.localeCompare(a.created_at)),
      }))
      .sort((a, b) => {
        const left = noteTitle(a.note)
        const right = noteTitle(b.note)
        const titleCompare = compareByText(left, right)
        if (titleCompare !== 0) return titleCompare
        return noteDate(b.note).localeCompare(noteDate(a.note))
      })
  }, [notesById, visibleItems])

  const loading = actionItems === null || notesById === null || people === null
  const hasItems = (actionItems?.length ?? 0) > 0
  const noActionItems = actionItems !== null && actionItems.length === 0 && actionItemsError === null
  const noMatches = hasItems && itemsByOwnerAndMeeting.length === 0
  const emptyForStatus = hasItems && itemsByOwnerAndMeeting.length > 0 && visibleItems.length === 0
  const showEmpty = noActionItems || noMatches || emptyForStatus
  const emptyTitle = noActionItems
    ? 'No action items yet'
    : noMatches
      ? 'No action items match these filters'
      : statusFilter === 'done'
        ? 'No done action items'
        : 'No open action items'
  const emptyHint = noActionItems
    ? 'Action items linked to meetings will appear here.'
    : noMatches
      ? 'Clear the owner or meeting filters to widen the list.'
      : statusFilter === 'done'
        ? 'There are no done action items for the current owner and meeting filters.'
        : 'There are no open action items for the current owner and meeting filters.'

  if (loading) {
    return (
      <div className="mx-auto max-w-4xl p-6">
        <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <div className="mb-2 h-7 w-44"><Skeleton className="h-full w-full" /></div>
            <div className="h-4 w-72"><Skeleton className="h-full w-full" /></div>
          </div>
          <div className="flex flex-col gap-2 sm:items-end">
            <div className="flex gap-2">
              <Skeleton className="h-9 w-40" />
              <Skeleton className="h-9 w-40" />
            </div>
            <div className="flex gap-2">
              <Skeleton className="h-9 w-16" />
              <Skeleton className="h-9 w-16" />
              <Skeleton className="h-9 w-16" />
            </div>
          </div>
        </div>
        <div className="flex flex-col gap-3">
          {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-28 w-full" />)}
        </div>
      </div>
    )
  }

  if (actionItemsError) {
    return (
      <div className="mx-auto max-w-4xl p-6">
        <div role="alert" className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
          <p className="font-medium">Could not load action items.</p>
          <p className="mt-1 break-words">{actionItemsError}</p>
        </div>
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-4xl p-6">
      <div className="mb-4 flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <h1 className="font-serif text-xl font-semibold">Action items</h1>
          <p className="text-sm text-muted-foreground">Across all meetings and notes.</p>
          {agentConfigured === false && statusCounts.open === 0 && statusCounts.done === 0 ? (
            <div className="mt-2"><AgentUnavailableNotice compact /></div>
          ) : (
            <p className="text-sm text-muted-foreground">
              {statusCounts.open} open - {statusCounts.done} done
            </p>
          )}
        </div>

        <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-end sm:justify-end">
          <label className="flex flex-col gap-1 text-sm text-muted-foreground">
            <span className="text-xs font-medium uppercase tracking-wide">Meeting</span>
            <select
              aria-label="Filter action items by meeting"
              value={meetingFilter}
              onChange={(e) => setMeetingFilter(e.target.value)}
              className="h-9 rounded-[var(--radius)] border border-input bg-background px-2 text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <option value={ALL_FILTER_VALUE}>All meetings</option>
              {meetingOptions.map(({ id, note }) => (
                <option key={id} value={id}>
                  {noteGroupLabel(note)}
                </option>
              ))}
            </select>
          </label>

          <label className="flex flex-col gap-1 text-sm text-muted-foreground">
            <span className="text-xs font-medium uppercase tracking-wide">Owner</span>
            <select
              aria-label="Filter action items by owner"
              value={ownerFilter}
              onChange={(e) => setOwnerFilter(e.target.value)}
              className="h-9 rounded-[var(--radius)] border border-input bg-background px-2 text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <option value={ALL_FILTER_VALUE}>All owners</option>
              <option value={UNASSIGNED}>Unassigned</option>
              {ownerOptions.map((person) => (
                <option key={person.id} value={person.id}>
                  {personLabel(person)}
                </option>
              ))}
            </select>
          </label>

          <div className="flex items-center gap-2" role="group" aria-label="Action item status filter">
            <button
              type="button"
              aria-pressed={statusFilter === 'open'}
              onClick={() => setStatusFilter('open')}
              className={[
                'rounded-[var(--radius)] border px-3 py-1.5 text-sm transition-colors',
                statusFilter === 'open'
                  ? 'border-primary bg-primary/10 text-foreground'
                  : 'border-border text-muted-foreground hover:bg-muted hover:text-foreground',
              ].join(' ')}
            >
              Open
            </button>
            <button
              type="button"
              aria-pressed={statusFilter === 'done'}
              onClick={() => setStatusFilter('done')}
              className={[
                'rounded-[var(--radius)] border px-3 py-1.5 text-sm transition-colors',
                statusFilter === 'done'
                  ? 'border-primary bg-primary/10 text-foreground'
                  : 'border-border text-muted-foreground hover:bg-muted hover:text-foreground',
              ].join(' ')}
            >
              Done
            </button>
            <button
              type="button"
              aria-pressed={statusFilter === 'all'}
              onClick={() => setStatusFilter('all')}
              className={[
                'rounded-[var(--radius)] border px-3 py-1.5 text-sm transition-colors',
                statusFilter === 'all'
                  ? 'border-primary bg-primary/10 text-foreground'
                  : 'border-border text-muted-foreground hover:bg-muted hover:text-foreground',
              ].join(' ')}
            >
              All
            </button>
          </div>
        </div>
      </div>

      {showEmpty ? (
        <EmptyState
          title={emptyTitle}
          hint={emptyHint}
        />
      ) : (
        <div className="flex flex-col gap-3">
          {groups.map((group) => (
            <NoteGroup key={group.noteId} noteId={group.noteId} note={group.note} items={group.items} peopleById={peopleById} />
          ))}
        </div>
      )}
    </div>
  )
}
