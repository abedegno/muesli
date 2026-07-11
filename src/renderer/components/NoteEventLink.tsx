import { useEffect, useState } from 'react'
import { Calendar, CalendarPlus } from 'lucide-react'
import { muesli } from '@/api'
import { formatEventTimeRange } from '@/lib/datetime'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from '@/components/ui/DropdownMenu'
import type { CalendarEvent } from '../../shared/types'

const DAY_MS = 86_400_000
// How far back/forward to search for the linked event's own details (there is
// no "get one calendar event by id" endpoint - see the lookup effect below).
// Wide enough to cover the realistic case (a note's meeting is usually close
// to `now`, whether the link was made right after recording or well after),
// while still bounded rather than an open-ended full-history fetch.
const LINKED_EVENT_LOOKUP_DAYS = 730

// Tri-state result of resolving the linked event's own details:
//  - undefined: not looked up yet (still loading, or no eventId)
//  - null: looked up, but not found within the lookup window (or the fetch
//    failed) - a distinct, honest "can't show details" state, never confused
//    with a real (but empty) title
//  - CalendarEvent: found
type LinkedEventLookup = CalendarEvent | null | undefined

/**
 * Note <-> calendar-event link affordance (CALLNK02). Reuses `getCalendarEvents`
 * (CALUI01/02) rather than a dedicated "get one event" endpoint, so it fetches a
 * window and looks the id up client-side:
 *  - the picker lists the next 7 days (same windowing convention as
 *    ComingUpScreen's "Coming up" view);
 *  - resolving a *linked* event's title/time widens the window to
 *    +/-LINKED_EVENT_LOOKUP_DAYS since the note's meeting (and its calendar
 *    event) is often not literally "upcoming" by the time it's viewed.
 * If the linked event genuinely falls outside that window (or the source no
 * longer has it), the UI still communicates that a link exists via a
 * distinct fallback rather than silently showing an inaccurate title/time.
 */
export function NoteEventLink({
  eventId,
  onLink,
  onUnlink,
}: {
  eventId?: string
  onLink: (eventId: string) => void
  onUnlink: () => void
}) {
  const [open, setOpen] = useState(false)
  const [upcoming, setUpcoming] = useState<CalendarEvent[] | null>(null)
  const [linkedEvent, setLinkedEvent] = useState<LinkedEventLookup>(undefined)

  useEffect(() => {
    if (!eventId) { setLinkedEvent(undefined); return }
    let cancelled = false
    setLinkedEvent(undefined)
    const now = Date.now()
    const from = new Date(now - LINKED_EVENT_LOOKUP_DAYS * DAY_MS).toISOString()
    const to = new Date(now + LINKED_EVENT_LOOKUP_DAYS * DAY_MS).toISOString()
    muesli
      .getCalendarEvents(from, to)
      .then((evs) => { if (!cancelled) setLinkedEvent(evs.find((e) => e.id === eventId) ?? null) })
      .catch(() => { if (!cancelled) setLinkedEvent(null) })
    return () => { cancelled = true }
  }, [eventId])

  const handleOpenChange = (next: boolean) => {
    setOpen(next)
    if (!next || upcoming !== null) return
    const now = new Date()
    const from = now.toISOString()
    const to = new Date(now.getTime() + 7 * DAY_MS).toISOString()
    muesli
      .getCalendarEvents(from, to)
      .then((evs) => setUpcoming(evs))
      .catch(() => setUpcoming([]))
  }

  if (eventId) {
    return (
      <div className="flex items-center gap-2 rounded-[var(--radius)] border border-border px-2 py-1 text-xs text-muted-foreground">
        <Calendar size={14} className="shrink-0" aria-hidden />
        <div className="min-w-0">
          {linkedEvent === undefined ? (
            <p className="truncate font-medium text-foreground">Loading linked event…</p>
          ) : linkedEvent === null ? (
            <p className="truncate font-medium text-foreground">Linked event (details unavailable)</p>
          ) : (
            <>
              <p className="truncate font-medium text-foreground">{linkedEvent.title || 'Untitled event'}</p>
              <p className="truncate">{formatEventTimeRange(linkedEvent)}</p>
            </>
          )}
        </div>
        <button
          type="button"
          aria-label="Unlink calendar event"
          onClick={onUnlink}
          className="ml-1 shrink-0 rounded px-1.5 py-0.5 hover:bg-muted hover:text-foreground"
        >
          Unlink
        </button>
      </div>
    )
  }

  return (
    <DropdownMenu open={open} onOpenChange={handleOpenChange}>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label="Link to calendar event"
          className="inline-flex items-center gap-1.5 rounded-[var(--radius)] border border-border px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <CalendarPlus size={14} aria-hidden />
          Link to event
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {upcoming === null ? (
          <DropdownMenuItem disabled>Loading…</DropdownMenuItem>
        ) : upcoming.length === 0 ? (
          <DropdownMenuItem disabled>No upcoming events</DropdownMenuItem>
        ) : (
          upcoming.map((ev) => (
            <DropdownMenuItem key={ev.id} onSelect={() => onLink(ev.id)}>
              <div className="min-w-0">
                <p className="truncate">{ev.title || 'Untitled event'}</p>
                <p className="truncate text-xs text-muted-foreground">{formatEventTimeRange(ev)}</p>
              </div>
            </DropdownMenuItem>
          ))
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
