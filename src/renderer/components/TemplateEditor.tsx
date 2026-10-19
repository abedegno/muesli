import { uiText } from '@/i18n'
import { useEffect, useState } from 'react'
import { Dialog } from '@/components/ui/Dialog'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import type { Template, TemplatePhase, TemplateSection } from '../../shared/types'

export function TemplateEditor({
  open,
  title,
  initial,
  onSave,
  onClose,
}: {
  open: boolean
  title: string
  initial?: Template
  onSave: (
    name: string,
    phase: TemplatePhase,
    sections: TemplateSection[],
    autoRun: boolean
  ) => Promise<void>
  onClose: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [phase, setPhase] = useState<TemplatePhase>(initial?.phase ?? 'after')
  const [autoRun, setAutoRun] = useState(initial?.auto_run ?? true)
  const [sections, setSections] = useState<TemplateSection[]>(
    initial?.sections ?? [{ heading: '', instruction: '' }]
  )
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) return
    setName(initial?.name ?? '')
    setPhase(initial?.phase ?? 'after')
    setAutoRun(initial?.auto_run ?? true)
    setSections(initial?.sections ?? [{ heading: '', instruction: '' }])
    setBusy(false)
  }, [initial, open])

  const valid =
    name.trim().length > 0 &&
    name.trim().length <= 80 &&
    sections.length > 0 &&
    sections.length <= 12 &&
    sections.every(
      (s) =>
        s.heading.trim().length > 0 &&
        s.heading.trim().length <= 80 &&
        s.instruction.trim().length > 0 &&
        s.instruction.trim().length <= 500
    )

  const update = (i: number, patch: Partial<TemplateSection>) =>
    setSections(sections.map((s, j) => (j === i ? { ...s, ...patch } : s)))
  const remove = (i: number) => setSections(sections.filter((_, j) => j !== i))
  const add = () => setSections([...sections, { heading: '', instruction: '' }])
  const swap = (a: number, b: number) => {
    const next = [...sections]
    ;[next[a], next[b]] = [next[b], next[a]]
    setSections(next)
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()} title={title}>
      <label htmlFor="template-editor-name" className="mb-3 block text-sm">
        <span className="mb-1 block font-medium">{uiText('Template name')}</span>
        <Input
          id="template-editor-name"
          aria-label={uiText('Template name')}
          value={name}
          maxLength={80}
          onChange={(e) => setName(e.target.value)}
        />
      </label>
      <label htmlFor="template-editor-phase" className="mb-3 block text-sm">
        <span className="mb-1 block font-medium">{uiText('Phase')}</span>
        <select
          id="template-editor-phase"
          aria-label={uiText('Phase')}
          value={phase}
          onChange={(e) => setPhase(e.target.value as TemplatePhase)}
          className="w-full rounded-[var(--radius)] border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <option value="after">{uiText('After')}</option>
          <option value="pre">{uiText('Pre')}</option>
          <option value="during">{uiText('During')}</option>
          <option value="cross">{uiText('Cross')}</option>
        </select>
      </label>
      <label htmlFor="template-editor-auto-run" className="mb-3 flex items-center gap-2 text-sm">
        <input
          id="template-editor-auto-run"
          aria-label={uiText('Auto-run on new notes')}
          type="checkbox"
          checked={autoRun}
          onChange={(e) => setAutoRun(e.target.checked)}
          className="h-4 w-4 rounded border border-input"
        />
        <span className="font-medium">{uiText('Auto-run on new notes')}</span>
      </label>
      <div className="mb-4 flex max-h-80 flex-col gap-3 overflow-y-auto">
        {sections.map((section, i) => (
          <div key={i} className="rounded-[var(--radius)] border border-border p-3">
            <Input
              aria-label={uiText('Section {value1} heading', { value1: i + 1 })}
              value={section.heading}
              maxLength={80}
              onChange={(e) => update(i, { heading: e.target.value })}
              placeholder={uiText('Heading')}
              className="mb-2"
            />
            <textarea
              aria-label={uiText('Section {value1} instruction', { value1: i + 1 })}
              value={section.instruction}
              maxLength={500}
              onChange={(e) => update(i, { instruction: e.target.value })}
              placeholder={uiText('Instruction')}
              rows={3}
              className="w-full rounded-[var(--radius)] border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
            <div className="mt-2 flex gap-2">
              <Button variant="ghost" size="sm" disabled={i === 0} onClick={() => swap(i, i - 1)}>
                {uiText('Move up')}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                disabled={i === sections.length - 1}
                onClick={() => swap(i, i + 1)}
              >
                {uiText('Move down')}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                aria-label={uiText('Remove section {value1}', { value1: i + 1 })}
                disabled={sections.length === 1}
                onClick={() => remove(i)}
              >
                {uiText('Remove')}
              </Button>
            </div>
          </div>
        ))}
        <Button
          variant="secondary"
          size="sm"
          className="self-start"
          onClick={add}
          disabled={sections.length >= 12}
        >
          {uiText('+ Add section')}
        </Button>
        {sections.length >= 12 && (
          <p className="text-xs text-muted-foreground">{uiText('Maximum 12 sections')}</p>
        )}
      </div>
      <div className="flex items-center justify-end gap-2">
        <Button variant="secondary" size="sm" onClick={onClose}>
          {uiText('Cancel')}
        </Button>
        <Button
          size="sm"
          disabled={!valid || busy}
          onClick={async () => {
            setBusy(true)
            try {
              await onSave(
                name.trim(),
                phase,
                sections.map((s) => ({
                  heading: s.heading.trim(),
                  instruction: s.instruction.trim(),
                })),
                autoRun
              )
              onClose()
            } catch {
              // save failed (caller showed a toast); keep the dialog open so edits aren't lost
            } finally {
              setBusy(false)
            }
          }}
        >
          {uiText('Save')}
        </Button>
      </div>
    </Dialog>
  )
}
