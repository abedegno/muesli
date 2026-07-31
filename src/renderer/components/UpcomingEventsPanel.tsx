import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/Button'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from './EmptyState'
import { muesli } from '@/api'
import { formatEventTimeRange } from '@/lib/datetime'
import { groupEventsByDay, type DayGroup } from '@/lib/comingUp'
import { Users, Video } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import type { CalendarEvent } from '../../shared/types'

const DAY_MS = 86_400_000
const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/** Human-readable day heading for a `YYYY-MM-DD` group date, e.g. 'Today', 'Tomorrow', 'Friday, Jul 11'. */
function dayHeading(dateKey: string, now: Date): string {
  const [y, m, d] = dateKey.split('-').map(Number)
  const day = new Date(y, m - 1, d)
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const diffDays = Math.round((day.getTime() - today.getTime()) / DAY_MS)
  if (diffDays === 0) return 'Today'
  if (diffDays === 1) return 'Tomorrow'
  return `${WEEKDAYS[day.getDay()]}, ${MONTHS[day.getMonth()]} ${day.getDate()}`
}

function EventRow({ event }: { event: CalendarEvent }) {
  const hasConferencing = typeof event.conferencing_url === 'string' && event.conferencing_url.length > 0
  return (
    <li className="flex items-center justify-between gap-3 rounded-[var(--radius)] border border-border px-3 py-2">
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{event.title || 'Untitled event'}</p>
        <p className="truncate text-xs text-muted-foreground">{formatEventTimeRange(event)}</p>
      </div>
      <div className="flex shrink-0 items-center gap-3 text-xs text-muted-foreground">
        <span className="flex items-center gap-1" title={`${event.attendees.length} attendee${event.attendees.length === 1 ? '' : 's'}`}>
          <Users size={14} /> {event.attendees.length}
        </span>
        {hasConferencing && (
          <span className="flex items-center gap-1 rounded-[var(--radius)] bg-primary/10 px-1.5 py-0.5 text-primary" title="Video call">
            <Video size={14} />
          </span>
        )}
      </div>
    </li>
  )
}

function DaySection({ group, now }: { group: DayGroup; now: Date }) {
  return (
    <section>
      <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
        {dayHeading(group.date, now)}
      </h2>
      {group.events.length === 0 ? (
        <p className="text-sm text-muted-foreground">No events</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {group.events.map((ev) => <EventRow key={ev.id} event={ev} />)}
        </ul>
      )}
    </section>
  )
}

export function UpcomingEventsPanel() {
  const navigate = useNavigate()
  const [events, setEvents] = useState<CalendarEvent[] | null>(null)
  const [now] = useState(() => new Date())

  useEffect(() => {
    let cancelled = false
    const from = now.toISOString()
    const to = new Date(now.getTime() + 7 * DAY_MS).toISOString()
    muesli
      .getCalendarEvents(from, to)
      .then((evs) => { if (!cancelled) setEvents(evs) })
      // No calendar configured, or the request otherwise failed — show the
      // graceful empty state rather than crashing or surfacing an error boundary.
      .catch(() => { if (!cancelled) setEvents([]) })
    return () => { cancelled = true }
  }, [now])

  if (events === null) {
    return (
      <div className="flex flex-col gap-2">
        <div className="mb-2 h-7 w-40"><Skeleton className="h-full w-full" /></div>
        {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}
      </div>
    )
  }

  const groups = groupEventsByDay(events, now)
  const isEmpty = events.length === 0

  return isEmpty ? (
    <EmptyState
      title="No upcoming events"
      hint="Connect a calendar, or check back once meetings are on your schedule."
      action={<Button variant="secondary" onClick={() => navigate('/settings#calendar')}>Open calendar settings</Button>}
    />
  ) : (
    <div className="flex flex-col gap-6">
      {groups.map((g) => <DaySection key={g.date} group={g} now={now} />)}
    </div>
  )
}
