import { useState } from 'react'
import { Dialog } from '@/components/ui/Dialog'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'

// A minimal controlled dialog for renaming a tag (modeled on FolderDialog). The
// rename cascades server-side to every note carrying the tag. onSave rejects on
// error (e.g. 409 duplicate); the caller surfaces it and the dialog stays open.
export function TagRenameDialog({
  open,
  initialName,
  onSave,
  onClose,
}: {
  open: boolean
  initialName: string
  onSave: (name: string) => Promise<void>
  onClose: () => void
}) {
  const [name, setName] = useState(initialName)
  const [busy, setBusy] = useState(false)
  const trimmed = name.trim()

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()} title="Rename tag">
      <Input
        aria-label="Tag name"
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="Tag name"
      />
      <p className="mt-3 text-xs text-muted-foreground">
        Renames the tag on every note that carries it.
      </p>
      <div className="mt-4 flex items-center justify-end gap-2">
        <Button variant="secondary" size="sm" onClick={onClose}>Cancel</Button>
        <Button size="sm" disabled={!trimmed || busy}
          onClick={async () => { setBusy(true); try { await onSave(trimmed); onClose() } catch { /* stay open */ } finally { setBusy(false) } }}>
          Save
        </Button>
      </div>
    </Dialog>
  )
}
