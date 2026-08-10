import { useState } from 'react'
import { Dialog } from '@/components/ui/Dialog'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import type { RuleGroup, RuleNode, RuleCondition, RuleField, RuleOperator, SmartList, Folder } from '../../shared/types'
import { isRuleGroup } from '../../shared/types'

const FIELD_OPS: Record<RuleField, RuleOperator[]> = {
  tag: ['is', 'isNot'],
  title: ['contains', 'equals'],
  status: ['is', 'isNot'],
  created: ['withinLastDays'],
  folder: ['is', 'isNot'],
}
const STATUSES = ['draft', 'recording', 'uploaded', 'transcribing', 'summarizing', 'ready', 'failed']
const newCondition = (): RuleCondition => ({ field: 'title', operator: 'contains', value: '' })

export function RuleEditor({
  open, title, initial, knownTags, knownFolders, onSave, onClose, onDelete,
}: {
  open: boolean
  title: string
  initial?: SmartList
  knownTags: string[]
  knownFolders?: Folder[]
  onSave: (name: string, rule: RuleGroup) => Promise<void>
  onClose: () => void
  onDelete?: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [rule, setRule] = useState<RuleGroup>(initial?.rule ?? { op: 'and', children: [] })
  const [busy, setBusy] = useState(false)
  const valid = name.trim().length > 0 && ruleIsComplete(rule)

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()} title={title}>
      <label htmlFor="rule-editor-name" className="mb-3 block text-sm">
        <span className="mb-1 block font-medium">List name</span>
        <Input id="rule-editor-name" aria-label="List name" value={name} onChange={(e) => setName(e.target.value)} />
      </label>
      <div className="mb-4 max-h-72 overflow-y-auto rounded-[var(--radius)] border border-border p-3">
        <GroupEditor group={rule} onChange={setRule} knownTags={knownTags} knownFolders={knownFolders} />
      </div>
      <div className="flex items-center justify-between">
        {onDelete ? <Button variant="destructive" size="sm" onClick={onDelete}>Delete</Button> : <span />}
        <div className="flex gap-2">
          <Button variant="secondary" size="sm" onClick={onClose}>Cancel</Button>
          <Button size="sm" disabled={!valid || busy}
            onClick={async () => {
              setBusy(true)
              try {
                await onSave(name.trim(), rule)
                onClose()
              } catch {
                // save failed (caller showed a toast); keep the dialog open so edits aren't lost
              } finally {
                setBusy(false)
              }
            }}>
            Save
          </Button>
        </div>
      </div>
    </Dialog>
  )
}

function ruleIsComplete(node: RuleNode): boolean {
  if (isRuleGroup(node)) return node.children.length === 0 || node.children.every(ruleIsComplete)
  if (node.field === 'created') return Number(node.value) > 0
  return String(node.value).trim().length > 0
}

function GroupEditor({ group, onChange, knownTags, knownFolders }: { group: RuleGroup; onChange: (g: RuleGroup) => void; knownTags: string[]; knownFolders?: Folder[] }) {
  const setChild = (i: number, n: RuleNode) => onChange({ ...group, children: group.children.map((c, j) => (j === i ? n : c)) })
  const remove = (i: number) => onChange({ ...group, children: group.children.filter((_, j) => j !== i) })
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2 text-xs">
        <span className="text-muted-foreground">Match</span>
        <select aria-label="group operator" value={group.op} onChange={(e) => onChange({ ...group, op: e.target.value as 'and' | 'or' })}
          className="rounded border border-input bg-background px-1 py-0.5">
          <option value="and">ALL (and)</option>
          <option value="or">ANY (or)</option>
        </select>
        <span className="text-muted-foreground">of:</span>
      </div>
      {group.children.map((child, i) => (
        <div key={i} className="flex items-start gap-2">
          {isRuleGroup(child)
            ? <div className="flex-1 rounded border border-border p-2"><GroupEditor group={child} onChange={(g) => setChild(i, g)} knownTags={knownTags} knownFolders={knownFolders} /></div>
            : <ConditionEditor cond={child} onChange={(c) => setChild(i, c)} knownTags={knownTags} knownFolders={knownFolders} />}
          <button aria-label="remove" onClick={() => remove(i)} className="text-xs text-muted-foreground hover:text-destructive">✕</button>
        </div>
      ))}
      <div className="flex gap-2">
        <Button variant="ghost" size="sm" onClick={() => onChange({ ...group, children: [...group.children, newCondition()] })}>+ add condition</Button>
        <Button variant="ghost" size="sm" onClick={() => onChange({ ...group, children: [...group.children, { op: 'and', children: [] }] })}>+ add group</Button>
      </div>
    </div>
  )
}

function ConditionEditor({ cond, onChange, knownTags, knownFolders }: { cond: RuleCondition; onChange: (c: RuleCondition) => void; knownTags: string[]; knownFolders?: Folder[] }) {
  const ops = FIELD_OPS[cond.field]
  return (
    <div className="flex flex-1 flex-wrap items-center gap-1 text-sm">
      <select aria-label="condition field" value={cond.field}
        onChange={(e) => { const field = e.target.value as RuleField; onChange({ field, operator: FIELD_OPS[field][0], value: '' }) }}
        className="rounded border border-input bg-background px-1 py-0.5">
        {(Object.keys(FIELD_OPS) as RuleField[]).map((f) => <option key={f} value={f}>{f}</option>)}
      </select>
      <select aria-label="condition operator" value={cond.operator} onChange={(e) => onChange({ ...cond, operator: e.target.value as RuleOperator })}
        className="rounded border border-input bg-background px-1 py-0.5">
        {ops.map((o) => <option key={o} value={o}>{o}</option>)}
      </select>
      {cond.field === 'status'
        ? <select aria-label="condition value" value={String(cond.value)} onChange={(e) => onChange({ ...cond, value: e.target.value })} className="rounded border border-input bg-background px-1 py-0.5">
            <option value="">—</option>{STATUSES.map((s) => <option key={s} value={s}>{s}</option>)}
          </select>
        : cond.field === 'created'
        ? <input aria-label="condition value" type="number" min={1} step={1} value={cond.value === '' ? '' : Number(cond.value)} onChange={(e) => onChange({ ...cond, value: e.target.value === '' ? '' : Math.trunc(Number(e.target.value)) })} className="w-16 rounded border border-input bg-background px-1 py-0.5" placeholder="days" />
        : cond.field === 'folder'
        ? <select aria-label="condition value" value={String(cond.value)} onChange={(e) => onChange({ ...cond, value: e.target.value })} className="rounded border border-input bg-background px-1 py-0.5">
            <option value="">—</option>{(knownFolders ?? []).map((f) => <option key={f.id} value={f.id}>{f.name}</option>)}
          </select>
        : <input aria-label="condition value" list={cond.field === 'tag' ? 're-tags' : undefined} value={String(cond.value)} onChange={(e) => onChange({ ...cond, value: e.target.value })} className="min-w-24 flex-1 rounded border border-input bg-background px-1 py-0.5" placeholder="value" />}
      {cond.field === 'tag' && <datalist id="re-tags">{knownTags.map((t) => <option key={t} value={t} />)}</datalist>}
    </div>
  )
}
