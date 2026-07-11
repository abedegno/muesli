// Pure helper for GET /api/calendar/events?from=&to= (CALUI01). Kept standalone
// so it's unit-testable in isolation, mirroring how buildNoteExportRequest is
// factored out in export.ts for muesliClient to consume.
export function buildCalendarEventsPath(from: string, to: string): string {
  return `/api/calendar/events?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`
}
