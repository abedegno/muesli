import { useState } from 'react'
import { Dialog } from '@/components/ui/Dialog'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'

export function FolderDialog({
  open,
  title,
  initialName = '',
  parentOptions = [],
  initialParentId,
  onSave,
  onDelete,
  onClose,
}: {
  open: boolean
  title: string
  initialName?: string
  parentOptions?: { id: string; name: string }[]
  initialParentId?: string | null
  onSave: (name: string, parentId: string | null) => Promise<void>
  onDelete?: () => Promise<void>
  onClose: () => void
}) {
  const [name, setName] = useState(initialName)
  const [parentId, setParentId] = useState<string | null>(initialParentId ?? null)
  const [busy, setBusy] = useState(false)
  const trimmed = name.trim()

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()} title={title}>
      <Input
        aria-label="Folder name"
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="Folder name"
      />
      <select
        aria-label="Parent folder"
        value={parentId ?? ''}
        onChange={(e) => setParentId(e.target.value || null)}
        className="mt-2 h-9 w-full rounded-[var(--radius)] border border-input bg-background px-2 text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <option value="">None (top level)</option>
        {parentOptions.map((f) => <option key={f.id} value={f.id}>{f.name}</option>)}
      </select>
      {onDelete && (
        <p className="mt-3 text-xs text-muted-foreground">
          This folder and everything inside it move to Trash — recoverable for 30 days.
        </p>
      )}
      <div className="mt-4 flex items-center justify-between">
        {onDelete ? (
          <Button variant="destructive" size="sm" disabled={busy}
            onClick={async () => { setBusy(true); try { await onDelete() } finally { setBusy(false) } }}>
            Move to Trash
          </Button>
        ) : <span />}
        <div className="flex gap-2">
          <Button variant="secondary" size="sm" onClick={onClose}>Cancel</Button>
          <Button size="sm" disabled={!trimmed || busy}
            onClick={async () => { setBusy(true); try { await onSave(trimmed, parentId); onClose() } catch { /* stay open */ } finally { setBusy(false) } }}>
            Save
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
