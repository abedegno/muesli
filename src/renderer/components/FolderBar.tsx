import { useEffect, useRef, useState } from 'react'
import { X, FolderPlus } from 'lucide-react'
import { cn } from '@/lib/cn'
import type { Folder } from '../../shared/types'

// FolderBar shows the folders a note is in as chips, with a picker to add an
// existing folder or create a new one. Async callbacks are owned by the parent.
export function FolderBar({
  folders,
  memberIds,
  onAdd,
  onCreate,
  onRemove,
}: {
  folders: Folder[]
  memberIds: string[]
  onAdd: (folderId: string) => Promise<void>
  onCreate: (name: string) => Promise<void>
  onRemove: (folderId: string) => Promise<void>
}) {
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState('')
  const pickerRef = useRef<HTMLDivElement>(null)
  const members = folders.filter((f) => memberIds.includes(f.id))
  const available = folders.filter((f) => !memberIds.includes(f.id))

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (!pickerRef.current || !pickerRef.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    // Defer the doc listener so the opening click doesn't immediately close it.
    const t = setTimeout(() => document.addEventListener('mousedown', onDoc), 0)
    document.addEventListener('keydown', onKey)
    return () => {
      clearTimeout(t)
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-border px-6 py-2">
      {members.map((f) => (
        <span key={f.id} className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs font-medium">
          {f.name}
          <button aria-label={`Remove ${f.name}`} onClick={() => void onRemove(f.id)} className="rounded-full hover:bg-foreground/10">
            <X size={12} />
          </button>
        </span>
      ))}
      <div className="relative" ref={pickerRef}>
        <button
          onClick={() => setOpen((o) => !o)}
          className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs text-muted-foreground hover:bg-muted"
        >
          <FolderPlus size={12} /> Add to folder
        </button>
        {open && (
          <div className={cn('absolute z-10 mt-1 w-48 rounded-[var(--radius)] border border-border bg-popover p-1 shadow-md')}>
            {available.map((f) => (
              <button
                key={f.id}
                onClick={async () => { setOpen(false); await onAdd(f.id) }}
                className="block w-full truncate rounded px-2 py-1 text-left text-sm hover:bg-muted"
              >
                {f.name}
              </button>
            ))}
            <input
              aria-label="New folder name"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={async (e) => {
                if (e.key === 'Enter' && draft.trim()) {
                  const name = draft.trim()
                  setDraft(''); setOpen(false)
                  await onCreate(name)
                }
              }}
              placeholder="New folder…"
              className="mt-1 w-full rounded border border-input bg-background px-2 py-1 text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          </div>
        )}
      </div>
    </div>
  )
}
