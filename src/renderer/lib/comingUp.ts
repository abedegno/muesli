import type { CalendarEvent } from '../../shared/types'

export interface DayGroup {
  /** Local-midnight day identifier, e.g. '2026-07-09'. */
  date: string
  events: CalendarEvent[]
}

/** YYYY-MM-DD for a Date's local calendar day (no UTC conversion). */
function dayKey(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

// Local calendar day `offsetDays` after `d` (0 = same day). Built from local
// calendar *fields* (year/month/day), not a fixed 24h millisecond offset:
// `Date` normalizes an out-of-range day-of-month across month and DST
// boundaries correctly, whereas adding 86_400_000ms to a local-midnight Date
// can land an hour off true midnight across a DST transition and collide
// with (or skip) an adjacent calendar day.
function addLocalDays(d: Date, offsetDays: number): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + offsetDays)
}

/**
 * Groups `events` into the next 7 local calendar days starting today
 * (today + 6 more), in chronological day order. Every day appears even when
 * it has no events (empty `events: []`); events within a day are sorted
 * chronologically by `starts_at`. Events outside the 7-day window (in the
 * past, or 7+ days out) are excluded. Pure — no fetching, no DOM access.
 */
export function groupEventsByDay(events: CalendarEvent[], now: Date): DayGroup[] {
  const days: DayGroup[] = []
  for (let i = 0; i < 7; i++) {
    days.push({ date: dayKey(addLocalDays(now, i)), events: [] })
  }
  const byKey = new Map(days.map((g) => [g.date, g]))

  for (const ev of events) {
    const starts = new Date(ev.starts_at)
    if (Number.isNaN(starts.getTime())) continue
    const key = dayKey(starts)
    const group = byKey.get(key)
    if (group) group.events.push(ev)
  }

  for (const group of days) {
    group.events.sort((a, b) => new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime())
  }

  return days
}
