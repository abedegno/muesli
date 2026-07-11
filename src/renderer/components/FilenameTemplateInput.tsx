import { renderFilenameTemplate, validateFilenameTemplate } from '../lib/filenameTemplate'

/** Dummy context used exclusively for the live preview. */
const PREVIEW_CTX = { date: '2024-01-15', title: 'Sample Meeting' }

/**
 * A controlled input for a filename template string.
 *
 * Renders:
 *  - A text <input> bound to `value` / `onChange`.
 *  - A live preview line rendered with dummy placeholder values.
 *  - An error message when the template contains unknown placeholders or
 *    would produce an empty filename.
 *
 * The `template` prop is accepted for API symmetry (e.g. a parent may pass
 * both the committed template and the in-progress edit value) but is not used
 * internally; validation and preview are derived from `value`.
 */
export function FilenameTemplateInput({
  value,
  onChange,
}: {
  value: string
  template?: string
  onChange: (value: string) => void
}) {
  const errors = validateFilenameTemplate(value)
  const preview = renderFilenameTemplate(value, PREVIEW_CTX)

  return (
    <div className="space-y-1">
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full rounded border border-border bg-background px-2 py-1 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
        aria-label="Filename template"
      />
      <p className="text-xs text-muted-foreground" aria-label="Preview">
        {preview || ' '}
      </p>
      {errors.length > 0 && (
        <p role="alert" className="text-xs text-destructive">
          {errors.join('; ')}
        </p>
      )}
    </div>
  )
}
