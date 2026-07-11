export const FILENAME_PLACEHOLDERS = ['date', 'title'] as const
export type FilenamePlaceholder = (typeof FILENAME_PLACEHOLDERS)[number]

export interface FilenameTemplateContext {
  date: string  // YYYY-MM-DD
  title: string // sanitized note title
}

/** Replace {date} and {title} in template with context values, then sanitize
 *  any characters that are invalid in filenames across common platforms. */
export function renderFilenameTemplate(
  template: string,
  ctx: FilenameTemplateContext,
): string {
  const rendered = template
    .replace(/\{date\}/g, ctx.date)
    .replace(/\{title\}/g, ctx.title)
  // Replace characters invalid in filenames (Windows + POSIX overlap)
  return rendered.replace(/[/\\:*?"<>|]/g, '_')
}

/** Return a list of validation error strings (empty array = valid). */
export function validateFilenameTemplate(template: string): string[] {
  const errors: string[] = []

  // Detect unknown {placeholder} tokens
  for (const [, name] of template.matchAll(/\{(\w+)\}/g)) {
    if (!FILENAME_PLACEHOLDERS.includes(name as FilenamePlaceholder)) {
      errors.push(`Unknown placeholder: {${name}}`)
    }
  }

  // Check that the rendered result is not effectively empty
  const dummy: FilenameTemplateContext = { date: '2024-01-15', title: 'Sample Meeting' }
  const rendered = renderFilenameTemplate(template, dummy).trim()
  if (!rendered) {
    errors.push('Filename template produces an empty result')
  }

  return errors
}
