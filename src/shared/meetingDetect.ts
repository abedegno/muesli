import type { CalendarEvent } from './types'
import type { CalendarPrefs } from './calendarPrefs'

// If multiple meetings are active at once, choose the one that started most
// recently. This makes overlapping meetings deterministic and biases toward the
// currently "most specific" active meeting.
export function detectActiveMeeting(
  events: CalendarEvent[],
  now: Date,
  settings: CalendarPrefs,
): CalendarEvent | null {
  void settings

  const nowMs = now.getTime()
  let winner: CalendarEvent | null = null
  let winnerStartMs = -Infinity

  for (const event of events) {
    if (!event.conferencing_url) continue

    const startMs = new Date(event.starts_at).getTime()
    const endMs = new Date(event.ends_at).getTime()
    if (Number.isNaN(startMs) || Number.isNaN(endMs)) continue
    if (nowMs < startMs || nowMs > endMs) continue

    if (winner === null || startMs > winnerStartMs) {
      winner = event
      winnerStartMs = startMs
    }
  }

  return winner
}
