/**
 * Builds the server path used by main to fetch a renderer-requested calendar snapshot.
 * `from` and `to` are passed through as caller-supplied RFC 3339 timestamps or dates.
 */
export function buildCalendarEventsPath(from: string, to: string): string {
  return `/api/calendar/events?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`
}
