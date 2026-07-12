import { useEffect, useRef, useState } from 'react'
import { muesli } from '@/api'
import type { ActionItem, Decision } from '../../shared/types'

export function NoteActionItemsPanel({ noteId }: { noteId: string }) {
  const [actionItems, setActionItems] = useState<ActionItem[] | null>(null)
  const [decisions, setDecisions] = useState<Decision[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [updatingIds, setUpdatingIds] = useState<Set<string>>(new Set())
  const noteIdRef = useRef(noteId)

  useEffect(() => {
    noteIdRef.current = noteId
  }, [noteId])

  useEffect(() => {
    if (!noteId) return

    let cancelled = false
    setActionItems(null)
    setDecisions(null)
    setError(null)
    setUpdatingIds(new Set())

    void muesli.listNoteActionItems(noteId)
      .then((res) => {
        if (cancelled || noteIdRef.current !== noteId) return
        setActionItems(res.actionItems ?? [])
        setDecisions(res.decisions ?? [])
      })
      .catch((err) => {
        if (cancelled || noteIdRef.current !== noteId) return
        setError(err instanceof Error ? err.message : 'Could not load action items')
      })

    return () => {
      cancelled = true
    }
  }, [noteId])

  if (!noteId) return null

  async function toggleActionItem(item: ActionItem) {
    const nextStatus = item.status === 'done' ? 'open' : 'done'
    const currentNoteId = noteId
    const previousItems = actionItems ?? []

    setUpdatingIds((cur) => {
      const next = new Set(cur)
      next.add(item.id)
      return next
    })
    setError(null)
    setActionItems((current) => current?.map((entry) => (entry.id === item.id ? { ...entry, status: nextStatus } : entry)) ?? current)

    try {
      const updated = await muesli.updateActionItem(item.id, { status: nextStatus })
      if (noteIdRef.current !== currentNoteId) return
      setActionItems((current) => current?.map((entry) => (entry.id === updated.id ? updated : entry)) ?? current)
    } catch (err) {
      if (noteIdRef.current !== currentNoteId) return
      setActionItems(previousItems)
      setError(err instanceof Error ? err.message : 'Could not update action item')
    } finally {
      if (noteIdRef.current !== currentNoteId) return
      setUpdatingIds((cur) => {
        const next = new Set(cur)
        next.delete(item.id)
        return next
      })
    }
  }

  const isLoading = actionItems === null && decisions === null && error === null

  return (
    <section className="border-b border-border px-6 py-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
          Action items and decisions
        </h2>
      </div>

      {error ? (
        <div role="alert" className="mt-3 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          Could not load action items.
          <div className="mt-1 text-xs text-destructive/80">{error}</div>
        </div>
      ) : isLoading ? (
        <div role="status" className="mt-3 text-sm text-muted-foreground">
          Loading action items...
        </div>
      ) : (
        <div className="mt-4 space-y-5">
          <div>
            <h3 className="text-sm font-medium">Action items</h3>
            {actionItems && actionItems.length > 0 ? (
              <ul className="mt-2 space-y-2">
                {actionItems.map((item) => {
                  const done = item.status === 'done'
                  return (
                    <li key={item.id} className="flex items-start gap-3 rounded-lg border border-border bg-background/70 px-3 py-2">
                      <input
                        type="checkbox"
                        aria-label={item.text}
                        checked={done}
                        disabled={updatingIds.has(item.id)}
                        onChange={() => {
                          void toggleActionItem(item)
                        }}
                        className="mt-1 h-4 w-4 rounded border-input text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                      />
                      <div className="min-w-0 flex-1">
                        <p className={done ? 'text-sm text-muted-foreground line-through' : 'text-sm text-foreground'}>
                          {item.text}
                        </p>
                        {item.due_hint ? (
                          <p className="mt-1 text-xs text-muted-foreground">Due: {item.due_hint}</p>
                        ) : null}
                      </div>
                    </li>
                  )
                })}
              </ul>
            ) : (
              <p className="mt-2 text-sm text-muted-foreground">No action items</p>
            )}
          </div>

          <div>
            <h3 className="text-sm font-medium">Decisions</h3>
            {decisions && decisions.length > 0 ? (
              <ul className="mt-2 space-y-2">
                {decisions.map((decision) => (
                  <li key={decision.id} className="rounded-lg border border-border bg-background/70 px-3 py-2 text-sm">
                    {decision.text}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-2 text-sm text-muted-foreground">No decisions</p>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
