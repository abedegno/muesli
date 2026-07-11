import { Button } from '@/components/ui/Button'
import type { CalendarEvent } from '../../../shared/types'

export function MeetingRecordPrompt({
  event,
  onAccept,
  onDismiss,
}: {
  event: CalendarEvent
  onAccept: () => void
  onDismiss: () => void
}) {
  return (
    <div role="status" aria-live="polite" className="sticky top-0 z-10 border-b border-border bg-accent/10 px-6 py-3 text-sm text-accent">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="font-medium text-foreground">Record {event.title || 'Untitled event'}?</p>
          <p className="text-xs text-muted-foreground">A meeting is in progress and can start recording now.</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button size="sm" type="button" onClick={onAccept}>Accept</Button>
          <Button size="sm" type="button" variant="secondary" onClick={onDismiss}>Dismiss</Button>
        </div>
      </div>
    </div>
  )
}
