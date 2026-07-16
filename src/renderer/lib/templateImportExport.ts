import type { Template, TemplatePhase, TemplateSection } from '../../shared/types'

export interface TemplateImportData {
  name: string
  phase: TemplatePhase
  sections: TemplateSection[]
  auto_run: boolean
}

const VALID_PHASES: TemplatePhase[] = ['after', 'pre', 'during', 'cross']

// Serializes only the portable recipe fields (name, phase, sections, auto_run)
// -- not id/built_in/agent-override fields -- so the output round-trips
// cleanly through muesli.createTemplate().
export function exportTemplateJSON(
  t: Pick<Template, 'name' | 'phase' | 'sections' | 'auto_run'>,
): string {
  const data: TemplateImportData = {
    name: t.name,
    phase: t.phase,
    sections: t.sections.map((s) => ({ heading: s.heading, instruction: s.instruction })),
    auto_run: t.auto_run,
  }
  return JSON.stringify(data, null, 2)
}

// Parses and validates a template JSON string for import. Throws a
// descriptive Error (shown in a toast by the caller) on any invalid shape.
export function parseTemplateImport(json: string): TemplateImportData {
  let raw: unknown
  try {
    raw = JSON.parse(json)
  } catch {
    throw new Error('Not valid JSON')
  }
  if (typeof raw !== 'object' || raw === null) {
    throw new Error('Template file must contain a JSON object')
  }
  const obj = raw as Record<string, unknown>
  if (typeof obj.name !== 'string' || obj.name.trim() === '') {
    throw new Error('Template name is missing or invalid')
  }
  const phase = typeof obj.phase === 'string' ? obj.phase : 'after'
  if (!VALID_PHASES.includes(phase as TemplatePhase)) {
    throw new Error(`Invalid phase "${phase}"`)
  }
  if (!Array.isArray(obj.sections) || obj.sections.length === 0) {
    throw new Error('Template must have at least one section')
  }
  const sections: TemplateSection[] = obj.sections.map((s, i) => {
    if (typeof s !== 'object' || s === null) throw new Error(`Section ${i + 1} is invalid`)
    const sec = s as Record<string, unknown>
    if (typeof sec.heading !== 'string' || sec.heading.trim() === '') {
      throw new Error(`Section ${i + 1} is missing a heading`)
    }
    if (typeof sec.instruction !== 'string' || sec.instruction.trim() === '') {
      throw new Error(`Section ${i + 1} is missing an instruction`)
    }
    return { heading: sec.heading, instruction: sec.instruction }
  })
  const autoRun = typeof obj.auto_run === 'boolean' ? obj.auto_run : true
  return { name: obj.name, phase: phase as TemplatePhase, sections, auto_run: autoRun }
}

export function templateExportFilename(name: string): string {
  const slug = name.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
  return `${slug || 'template'}.json`
}

// Triggers a browser download of `content` as `filename`. Isolated behind
// this function so component tests can mock it instead of exercising
// Blob/URL/anchor-click DOM plumbing.
export function triggerDownload(
  filename: string,
  content: string,
  mimeType = 'application/json',
): void {
  const blob = new Blob([content], { type: mimeType })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}
