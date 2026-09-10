import { useState } from 'react'
import { Dialog } from '@/components/ui/Dialog'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import type { FolderVisibility } from '../../shared/types'

export function FolderDialog({
  open,
  title,
  initialName = '',
  parentOptions = [],
  initialParentId,
  onSave,
  onDelete,
  onClose,
  visibility,
  showVisibilityToggle = false,
  onSetVisibility,
}: {
  open: boolean
  title: string
  initialName?: string
  parentOptions?: { id: string; name: string }[]
  initialParentId?: string | null
  onSave: (name: string, parentId: string | null) => Promise<void>
  onDelete?: () => Promise<void>
  onClose: () => void
  /** Current folder visibility; only meaningful when showVisibilityToggle is true. */
  visibility?: FolderVisibility
  /** Show the "Shared with team" toggle. Callers gate this on the folder
   * being owned by the current user and team_sharing_available being true
   * (single-user deployments hide the toggle; issue #12). */
  showVisibilityToggle?: boolean
  onSetVisibility?: (visibility: FolderVisibility) => Promise<void>
}) {
  const [name, setName] = useState(initialName)
  const [parentId, setParentId] = useState<string | null>(initialParentId ?? null)
  const [busy, setBusy] = useState(false)
  const [visBusy, setVisBusy] = useState(false)
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
      {showVisibilityToggle && onSetVisibility && (
        <label className="mt-3 flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            aria-label="Shared with team"
            checked={visibility === 'shared'}
            disabled={visBusy}
            onChange={async (e) => {
              const next: FolderVisibility = e.target.checked ? 'shared' : 'private'
              setVisBusy(true)
              try {
                await onSetVisibility(next)
              } finally {
                setVisBusy(false)
              }
            }}
          />
          Shared with team
        </label>
      )}
      {showVisibilityToggle && (
        <p className="mt-1 text-xs text-muted-foreground">
          Notes filed directly in this folder become read-only visible to everyone on this deployment. Sharing does not grant write access.
        </p>
      )}
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
