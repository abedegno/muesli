import { UpcomingEventsPanel } from './UpcomingEventsPanel'

export function ComingUpScreen() {
  return (
    <div className="mx-auto max-w-3xl p-6">
      <h1 className="mb-1 font-serif text-xl font-semibold">Coming up</h1>
      <p className="mb-4 text-sm text-muted-foreground">Your next 7 days.</p>
      <UpcomingEventsPanel />
    </div>
  )
}
