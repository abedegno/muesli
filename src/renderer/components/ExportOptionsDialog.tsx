import { useEffect, useState } from 'react'
import { Dialog } from './ui/Dialog'
import { Button } from './ui/Button'

export interface ExportFormatOption {
  value: string
  label: string
}

export interface ExportOptionsValue {
  format: string
  includeTranscript: boolean
  redactSpeakers: boolean
}

export function ExportOptionsDialog({
  open,
  title,
  description,
  formats,
  initialFormat,
  confirmLabel,
  onCancel,
  onConfirm,
}: {
  open: boolean
  title: string
  description?: string
  formats: ExportFormatOption[]
  initialFormat?: string
  confirmLabel: string
  onCancel: () => void
  onConfirm: (value: ExportOptionsValue) => Promise<void> | void
}) {
  const defaultFormat = initialFormat && formats.some((format) => format.value === initialFormat)
    ? initialFormat
    : formats[0]?.value ?? ''
  const [format, setFormat] = useState(defaultFormat)
  const [includeTranscript, setIncludeTranscript] = useState(true)
  const [redactSpeakers, setRedactSpeakers] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!open) return
    setFormat(defaultFormat)
    setIncludeTranscript(true)
    setRedactSpeakers(false)
    setSubmitting(false)
  }, [defaultFormat, open])

  const submit = async () => {
    if (!format) return
    setSubmitting(true)
    try {
      await onConfirm({ format, includeTranscript, redactSpeakers })
      setSubmitting(false)
      onCancel()
    } catch {
      // Caller is responsible for surfacing any error toast.
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) onCancel() }} title={title}>
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
        <label className="block space-y-1 text-sm">
          <span className="font-medium">Format</span>
          <select
            aria-label="Export format"
            value={format}
            onChange={(e) => setFormat(e.target.value)}
            className="h-9 w-full rounded-[var(--radius)] border border-border bg-background px-3 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
          >
            {formats.map((option) => (
              <option key={option.value} value={option.value}>{option.label}</option>
            ))}
          </select>
        </label>
        <label className="flex items-start gap-3 rounded-[var(--radius)] border border-border bg-background/70 px-3 py-2 text-sm">
          <input
            type="checkbox"
            checked={includeTranscript}
            onChange={(e) => setIncludeTranscript(e.target.checked)}
            className="mt-1 h-4 w-4 rounded border-border text-primary focus:ring-primary"
          />
          <span>
            <span className="block font-medium">Include transcript</span>
            <span className="block text-xs text-muted-foreground">Export the transcript along with the note body.</span>
          </span>
        </label>
        <label className="flex items-start gap-3 rounded-[var(--radius)] border border-border bg-background/70 px-3 py-2 text-sm">
          <input
            type="checkbox"
            checked={redactSpeakers}
            onChange={(e) => setRedactSpeakers(e.target.checked)}
            className="mt-1 h-4 w-4 rounded border-border text-primary focus:ring-primary"
          />
          <span>
            <span className="block font-medium">Redact speaker names</span>
            <span className="block text-xs text-muted-foreground">Replace speaker labels with neutral placeholders.</span>
          </span>
        </label>
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" variant="secondary" size="sm" onClick={onCancel} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" size="sm" disabled={submitting || !format}>
            {submitting ? 'Exporting...' : confirmLabel}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
