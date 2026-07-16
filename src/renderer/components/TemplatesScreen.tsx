import { useEffect, useRef, useState } from 'react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { Dialog } from '@/components/ui/Dialog'
import { useToast } from '@/components/ui/Toast'
import {
  exportTemplateJSON,
  parseTemplateImport,
  templateExportFilename,
  triggerDownload,
} from '@/lib/templateImportExport'
import { TemplateEditor } from './TemplateEditor'
import type { Template, TemplatePhase } from '../../shared/types'

function phaseLabel(phase: TemplatePhase): string {
  switch (phase) {
    case 'after':
      return 'After'
    case 'pre':
      return 'Pre'
    case 'during':
      return 'During'
    case 'cross':
      return 'Cross'
  }
  return phase
}

function uniqueDuplicateName(base: string, existing: string[]): string {
  const taken = new Set(existing.map((n) => n.toLowerCase()))
  let candidate = `${base} (copy)`
  let n = 2
  while (taken.has(candidate.toLowerCase())) {
    candidate = `${base} (copy ${n})`
    n += 1
  }
  return candidate
}

async function readTemplateFileText(file: File): Promise<string> {
  if (typeof file.text === 'function') {
    return file.text()
  }

  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result ?? ''))
    reader.onerror = () => reject(reader.error ?? new Error('Could not read file'))
    reader.readAsText(file)
  })
}

export function TemplatesScreen() {
  const [templates, setTemplates] = useState<Template[]>([])
  const [editing, setEditing] = useState<{ template?: Template } | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<Template | null>(null)
  const [previewing, setPreviewing] = useState<Template | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const { notify } = useToast()

  const reload = () => muesli.listTemplates().then(setTemplates)

  useEffect(() => {
    reload()
  }, [])

  const builtIns = templates.filter((t) => t.built_in)
  const mine = templates.filter((t) => !t.built_in)
  const duplicate = async (t: Template) => {
    const name = uniqueDuplicateName(t.name, mine.map((m) => m.name))
    try {
      await muesli.createTemplate(name, t.phase, t.sections, t.auto_run)
      await reload()
      notify(`Duplicated as "${name}"`, 'info')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not duplicate template', 'error')
    }
  }

  return (
    <div className="mx-auto max-w-2xl p-6">
      <h1 className="mb-6 font-serif text-3xl">Templates</h1>

      <section className="mb-8">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-medium uppercase tracking-wide text-muted-foreground">
            Your templates
          </h2>
          <div className="flex items-center gap-2">
            <input
              ref={fileInputRef}
              type="file"
              accept="application/json"
              aria-label="Import template file"
              className="hidden"
              onChange={async (e) => {
                const file = e.target.files?.[0]
              e.target.value = ''
              if (!file) return
              try {
                  const text = await readTemplateFileText(file)
                  const data = parseTemplateImport(text)
                  await muesli.createTemplate(data.name, data.phase, data.sections, data.auto_run)
                  await reload()
                  notify(`Imported "${data.name}"`, 'info')
                } catch (err) {
                  notify(err instanceof Error ? err.message : 'Could not import template', 'error')
                }
              }}
            />
            <Button size="sm" variant="secondary" onClick={() => fileInputRef.current?.click()}>
              Import template
            </Button>
            <Button size="sm" onClick={() => setEditing({})}>
              + New template
            </Button>
          </div>
        </div>
        {mine.length === 0 ? (
          <p className="text-sm text-muted-foreground">No custom templates yet.</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {mine.map((t) => (
              <li
                key={t.id}
                className="flex items-center justify-between rounded-[var(--radius)] border border-border p-3"
              >
                <div>
                  <div className="font-medium">{t.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {t.sections.length} section{t.sections.length === 1 ? '' : 's'}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Badge>{phaseLabel(t.phase)}</Badge>
                  <Badge tone={t.auto_run ? 'primary' : 'neutral'}>{t.auto_run ? 'Auto-run' : 'Manual'}</Badge>
                  <Button variant="secondary" size="sm" aria-label={`Duplicate ${t.name}`} onClick={() => duplicate(t)}>
                    Duplicate
                  </Button>
                  <Button
                    variant="secondary"
                    size="sm"
                    aria-label={`Export ${t.name}`}
                    onClick={() => triggerDownload(templateExportFilename(t.name), exportTemplateJSON(t))}
                  >
                    Export
                  </Button>
                  <Button variant="secondary" size="sm" onClick={() => setEditing({ template: t })}>
                    Edit
                  </Button>
                  <Button
                    variant="destructive"
                    size="sm"
                    aria-label={`Delete ${t.name}`}
                    onClick={() => setConfirmDelete(t)}
                  >
                    Delete
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>

      {builtIns.length > 0 && (
        <section>
          <h2 className="mb-3 text-sm font-medium uppercase tracking-wide text-muted-foreground">
            Built-in
          </h2>
          <ul className="flex flex-col gap-2">
            {builtIns.map((t) => (
              <li
                key={t.id}
                className="flex items-center justify-between rounded-[var(--radius)] border border-border p-3"
              >
                <div className="flex items-center gap-2">
                  <span className="font-medium">{t.name}</span>
                  <Badge>{phaseLabel(t.phase)}</Badge>
                </div>
                <div className="flex items-center gap-2">
                  <Button variant="ghost" size="sm" aria-label={`View ${t.name} sections`} onClick={() => setPreviewing(t)}>
                    View
                  </Button>
                  <Badge tone={t.auto_run ? 'primary' : 'neutral'}>{t.auto_run ? 'Auto-run' : 'Manual'}</Badge>
                  <Button variant="secondary" size="sm" aria-label={`Duplicate ${t.name}`} onClick={() => duplicate(t)}>
                    Duplicate
                  </Button>
                  <Button
                    variant="secondary"
                    size="sm"
                    aria-label={`Export ${t.name}`}
                    onClick={() => triggerDownload(templateExportFilename(t.name), exportTemplateJSON(t))}
                  >
                    Export
                  </Button>
                  <Badge>Built-in</Badge>
                </div>
              </li>
            ))}
          </ul>
        </section>
      )}

      {editing && (
        <TemplateEditor
          open
          title={editing.template ? 'Edit template' : 'New template'}
          initial={editing.template}
          onSave={async (name, phase, sections, autoRun) => {
            try {
              if (editing.template) await muesli.updateTemplate(editing.template.id, name, phase, sections, autoRun)
              else await muesli.createTemplate(name, phase, sections, autoRun)
              await reload()
            } catch (err) {
              notify(err instanceof Error ? err.message : 'Could not save template', 'error')
              throw err
            }
          }}
          onClose={() => setEditing(null)}
        />
      )}

      {confirmDelete !== null && (
        <Dialog
          open
          onOpenChange={(o) => { if (!o) setConfirmDelete(null) }}
          title="Delete template?"
        >
          <p className="text-sm text-muted-foreground">
            Delete &ldquo;{confirmDelete.name}&rdquo;? This cannot be undone.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" size="sm" onClick={() => setConfirmDelete(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={async () => {
                try {
                  await muesli.deleteTemplate(confirmDelete.id)
                  await reload()
                  setConfirmDelete(null)
                } catch (err) {
                  notify(err instanceof Error ? err.message : 'Could not delete template', 'error')
                  setConfirmDelete(null)
                }
              }}
            >
              Delete
            </Button>
          </div>
        </Dialog>
      )}

      {previewing !== null && (
        <Dialog
          open
          onOpenChange={(o) => { if (!o) setPreviewing(null) }}
          title={previewing.name}
        >
          <div className="mb-3 flex items-center gap-2">
            <Badge>{phaseLabel(previewing.phase)}</Badge>
            <Badge tone={previewing.auto_run ? 'primary' : 'neutral'}>
              {previewing.auto_run ? 'Auto-run' : 'Manual'}
            </Badge>
          </div>
          <ul className="flex max-h-80 flex-col gap-3 overflow-y-auto">
            {previewing.sections.map((s, i) => (
              <li key={i} className="rounded-[var(--radius)] border border-border p-3">
                <div className="mb-1 font-medium">{s.heading}</div>
                <div className="text-sm text-muted-foreground">{s.instruction}</div>
              </li>
            ))}
          </ul>
          <div className="mt-4 flex justify-end">
            <Button variant="secondary" size="sm" onClick={() => setPreviewing(null)}>
              Close
            </Button>
          </div>
        </Dialog>
      )}
    </div>
  )
}
