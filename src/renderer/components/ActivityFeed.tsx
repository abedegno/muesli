import { useActivity } from '@/lib/activityStore'
import { statusLabel } from '@/lib/status'
import type { UploadPhase } from '@/lib/activityStore'

function uploadPhaseLabel(phase: UploadPhase): string {
  switch (phase) {
    case 'requesting-url':
      return 'Preparing upload…'
    case 'uploading-audio':
      return 'Uploading audio…'
    case 'confirming-upload':
      return 'Confirming…'
    case 'done':
      return 'Upload complete'
    case 'error':
      return 'Upload failed'
  }
}

export function ActivityFeed() {
  const { items, dismiss } = useActivity()

  if (items.length === 0) return null

  return (
    <div
      aria-live="polite"
      className="fixed bottom-20 right-4 z-50 flex flex-col gap-2"
    >
      {items.map((item) => {
        const label =
          item.kind === 'upload'
            ? `${item.noteTitle} — ${uploadPhaseLabel(item.phase)}`
            : `${item.noteTitle} — ${statusLabel(item.status)}`

        return (
          <div
            key={item.id}
            className="flex items-center gap-2 rounded-[var(--radius)] border border-border bg-popover px-3 py-2 text-sm shadow-md"
          >
            {item.done && (
              <span className="text-accent" aria-label="complete">
                ✓
              </span>
            )}
            <span className="flex-1 text-foreground">{label}</span>
            <button
              aria-label="Dismiss"
              onClick={() => dismiss(item.id)}
              className="ml-2 text-muted-foreground hover:text-foreground"
            >
              ×
            </button>
          </div>
        )
      })}
    </div>
  )
}
