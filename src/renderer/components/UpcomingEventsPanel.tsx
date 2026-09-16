import { useEffect, useId, useState } from 'react'
import { Button } from '@/components/ui/Button'
import { Skeleton } from '@/components/ui/Skeleton'
import { EmptyState } from './EmptyState'
import { Markdown } from './Markdown'
import { muesli } from '@/api'
import { formatEventTimeRange } from '@/lib/datetime'
import { groupEventsByDay, type DayGroup } from '@/lib/comingUp'
import { ChevronDown, Users, Video } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import type { CalendarEvent, EventBrief } from '../../shared/types'

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

// briefStatusLabel renders a short, human-readable label for a brief's
// state. Internal diagnostics (job errors, hashes) are never available here
// -- the API never exposes them -- so a failed brief gets a generic message.
function briefStatusLabel(status: EventBrief['status']): string {
  switch (status) {
    case 'pending':
      return 'Generating\u2026'
    case 'failed':
      return 'Could not be generated.'
    default:
      return ''
  }
}

/** One template's brief panel: a labeled heading plus its state. Ready
 * brief sections render through the existing sanitized Markdown renderer
 * (Markdown builds React elements directly, never dangerouslySetInnerHTML,
 * so hostile content in a section can never inject markup). */
function BriefPanel({ brief }: { brief: EventBrief }) {
  return (
    <section aria-label={`${brief.template_name} brief`} className="rounded-[var(--radius)] border border-border/60 bg-muted/30 p-3">
      <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{brief.template_name}</h3>
      {brief.status === 'ready' ? (
        <div className="mt-1 text-sm">
          {brief.sections.map((section, i) => (
            <div key={i}>
              {section.heading && <h4 className="mt-2 text-sm font-medium first:mt-0">{section.heading}</h4>}
              <Markdown source={section.content_markdown} />
            </div>
          ))}
        </div>
      ) : (
        <p className="mt-1 text-sm text-muted-foreground">{briefStatusLabel(brief.status)}</p>
      )}
    </section>
  )
}

/** Expandable "Brief" area for one event, shown only when at least one
 * applicable brief exists (an absent `briefs` field, for rolling-upgrade
 * compatibility with an older server, is treated as empty -- see the
 * `event.briefs ?? []` default below). Multiple templates render as
 * separate panels once expanded. The toggle is a real <button> with
 * aria-expanded/aria-controls so it is reachable and operable by keyboard
 * and announced correctly by assistive tech. */
function EventBriefs({ event }: { event: CalendarEvent }) {
  const briefs = event.briefs ?? []
  const [expanded, setExpanded] = useState(false)
  const panelId = useId()

  if (briefs.length === 0) return null

  return (
    <div className="mt-2 border-t border-border/60 pt-2">
      <button
        type="button"
        className="flex w-full items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground"
        aria-expanded={expanded}
        aria-controls={panelId}
        onClick={() => setExpanded((e) => !e)}
      >
        <ChevronDown size={14} className={expanded ? 'rotate-0 transition-transform' : '-rotate-90 transition-transform'} />
        Brief {expanded ? '' : `(${briefs.length})`}
      </button>
      {expanded && (
        <div id={panelId} className="mt-2 flex flex-col gap-2">
          {briefs.map((brief) => (
            <BriefPanel key={brief.id} brief={brief} />
          ))}
        </div>
      )}
    </div>
  )
}

function EventRow({ event }: { event: CalendarEvent }) {
  const hasConferencing = typeof event.conferencing_url === 'string' && event.conferencing_url.length > 0
  return (
    <li className="flex flex-col gap-1 rounded-[var(--radius)] border border-border px-3 py-2">
      <div className="flex items-center justify-between gap-3">
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
      </div>
      <EventBriefs event={event} />
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
