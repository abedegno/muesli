import type { CalendarEvent, Note } from '../../shared/types'
import { sortNotesPinnedFirst } from './noteOrdering'

const DAY = 86_400_000
const WEEKDAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

const startOfDay = (d: Date): number => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()

// Day-bucket label for a note's created_at relative to `now`, by *calendar* day.
// today → 'Today'; yesterday → 'Yesterday'; 2–6 days back → weekday; older → 'MMM D'
// (with a year suffix when not the current year). Invalid/empty dates → 'Earlier'.
export function dayBucket(createdAt: string, now: Date): string {
  const created = new Date(createdAt)
  if (Number.isNaN(created.getTime())) return 'Earlier'
  const diffDays = Math.round((startOfDay(now) - startOfDay(created)) / DAY)
  if (diffDays <= 0) return 'Today'
  if (diffDays === 1) return 'Yesterday'
  if (diffDays < 7) return WEEKDAYS[created.getDay()]
  const base = `${MONTHS[created.getMonth()]} ${created.getDate()}`
  return created.getFullYear() === now.getFullYear() ? base : `${base}, ${created.getFullYear()}`
}

// Semantic group label for notes in groupNotesByDate.
// Extends dayBucket's 0-6 day logic with Last week / This month / Older.
function groupLabel(createdAt: string, now: Date): string {
  const created = new Date(createdAt)
  if (Number.isNaN(created.getTime())) return 'Earlier'
  const diffDays = Math.round((startOfDay(now) - startOfDay(created)) / DAY)
  if (diffDays <= 0) return 'Today'
  if (diffDays === 1) return 'Yesterday'
  if (diffDays < 7) return WEEKDAYS[created.getDay()]
  if (diffDays < 14) return 'Last week'
  if (created.getMonth() === now.getMonth() && created.getFullYear() === now.getFullYear()) {
    return 'This month'
  }
  return 'Older'
}

// Locale short time, e.g. '5:15 PM'. Empty string for invalid dates.
export function formatTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })
}

// 'starts–ends' human-readable range for a calendar event, e.g.
// '2:00 PM – 2:30 PM' (shared by ComingUpScreen and the note<->event link UI
// so event times read identically everywhere they appear).
export function formatEventTimeRange(ev: CalendarEvent): string {
  const start = formatTime(ev.starts_at)
  const end = formatTime(ev.ends_at)
  if (!start && !end) return ''
  if (!end) return start
  return `${start} – ${end}`
}

// Group notes into a leading pinned bucket plus ordered day buckets for the
// remaining notes. The pinned section preserves the existing note ordering
// within that partition; the date buckets keep their own calendar grouping.
export function groupNotesByDate(notes: Note[], now: Date): { label: string; notes: Note[] }[] {
  const time = (iso: string): number => {
    const ms = new Date(iso).getTime()
    return Number.isNaN(ms) ? 0 : ms
  }
  const sorted = sortNotesPinnedFirst([...notes].sort((a, b) => time(b.created_at) - time(a.created_at)))
  const pinned = sorted.filter((n) => n.pinned)
  const dated = sorted.filter((n) => !n.pinned)
  const groups: { label: string; notes: Note[] }[] = []
  if (pinned.length > 0) {
    groups.push({ label: 'Pinned', notes: pinned })
  }
  for (const n of dated) {
    const label = groupLabel(n.created_at, now)
    const last = groups[groups.length - 1]
    if (last && last.label === label) last.notes.push(n)
    else groups.push({ label, notes: [n] })
  }
  return groups
}

// defaultMeetingTitle is the human-friendly title a new meeting gets, e.g.
// "Meeting — Jun 13, 2:10 PM" (locale-aware; far cleaner than a raw toLocaleString).
export function defaultMeetingTitle(now: Date): string {
  const date = now.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  return `Meeting — ${date}, ${formatTime(now.toISOString())}`
}
