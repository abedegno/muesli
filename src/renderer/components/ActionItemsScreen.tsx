import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { muesli } from '@/api'
import { EmptyState } from './EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import type { ActionItem, Note } from '../../shared/types'

type StatusFilter = 'open' | 'all'

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

function ActionItemRow({ item }: { item: ActionItem }) {
  const done = item.status === 'done'

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
}: {
  noteId: string
  note?: Note
  items: ActionItem[]
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
        {items.map((item) => <ActionItemRow key={item.id} item={item} />)}
      </ul>
    </section>
  )
}

export function ActionItemsScreen() {
  const [notesById, setNotesById] = useState<Record<string, Note> | null>(null)
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('open')
  const [actionItems, setActionItems] = useState<ActionItem[] | null>(null)
  const [actionItemsError, setActionItemsError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setNotesById(null)
    setActionItems(null)
    setActionItemsError(null)

    void Promise.all([muesli.listNotes(), muesli.listActionItems(statusFilter)])
      .then(([notes, items]) => {
        if (cancelled) return
        const next: Record<string, Note> = {}
        for (const note of notes ?? []) {
          next[note.id] = note
        }
        setNotesById(next)
        setActionItems(items ?? [])
      })
      .catch((err) => {
        if (cancelled) return
        setNotesById({})
        setActionItems([])
        setActionItemsError(errorMessage(err))
      })

    return () => {
      cancelled = true
    }
  }, [statusFilter])

  const groups = useMemo(() => {
    const byNote = new Map<string, ActionItem[]>()
    for (const item of actionItems ?? []) {
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
  }, [actionItems, notesById])

  const loading = actionItems === null || notesById === null
  const empty = actionItems !== null && actionItems.length === 0 && actionItemsError === null
  const visibleCount = actionItems?.length ?? 0

  if (loading) {
    return (
      <div className="mx-auto max-w-4xl p-6">
        <div className="mb-4 flex items-end justify-between gap-3">
          <div>
            <div className="mb-2 h-7 w-44"><Skeleton className="h-full w-full" /></div>
            <div className="h-4 w-72"><Skeleton className="h-full w-full" /></div>
          </div>
          <div className="flex gap-2">
            <Skeleton className="h-9 w-16" />
            <Skeleton className="h-9 w-16" />
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
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 className="font-serif text-xl font-semibold">Action items</h1>
          <p className="text-sm text-muted-foreground">Across all meetings and notes.</p>
          <p className="text-sm text-muted-foreground">{visibleCount} item{visibleCount === 1 ? '' : 's'}</p>
        </div>

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

      {empty ? (
        <EmptyState
          title={statusFilter === 'open' ? 'No open action items' : 'No action items'}
          hint={statusFilter === 'open'
            ? 'Nothing is currently open across your notes.'
            : 'There are no action items yet.'}
        />
      ) : (
        <div className="flex flex-col gap-3">
          {groups.map((group) => (
            <NoteGroup key={group.noteId} noteId={group.noteId} note={group.note} items={group.items} />
          ))}
        </div>
      )}
    </div>
  )
}
