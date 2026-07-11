import { useEffect, useState } from 'react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { Badge } from '@/components/ui/Badge'
import { Dialog } from '@/components/ui/Dialog'
import { useToast } from '@/components/ui/Toast'
import { TemplateEditor } from './TemplateEditor'
import type { Template } from '../../shared/types'

export function TemplatesScreen() {
  const [templates, setTemplates] = useState<Template[]>([])
  const [editing, setEditing] = useState<{ template?: Template } | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<Template | null>(null)
  const { notify } = useToast()

  const reload = () => muesli.listTemplates().then(setTemplates)

  useEffect(() => {
    reload()
  }, [])

  const builtIns = templates.filter((t) => t.built_in)
  const mine = templates.filter((t) => !t.built_in)

  return (
    <div className="mx-auto max-w-2xl p-6">
      <h1 className="mb-6 font-serif text-3xl">Templates</h1>

      <section className="mb-8">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-medium uppercase tracking-wide text-muted-foreground">
            Your templates
          </h2>
          <Button size="sm" onClick={() => setEditing({})}>
            + New template
          </Button>
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
                <div className="flex gap-2">
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
                <span className="font-medium">{t.name}</span>
                <Badge>Built-in</Badge>
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
          onSave={async (name, sections) => {
            try {
              if (editing.template) await muesli.updateTemplate(editing.template.id, name, sections)
              else await muesli.createTemplate(name, sections)
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
    </div>
  )
}
