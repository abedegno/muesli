import { useCallback, useEffect, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { Dialog } from '@/components/ui/Dialog'
import { EmptyState } from './EmptyState'
import { Skeleton } from '@/components/ui/Skeleton'
import { useToast } from '@/components/ui/Toast'
import type { Folder, Note, SmartList } from '../../shared/types'

interface Ctx { refresh: () => void }

/** Returns a human-readable label for how long ago a date was, e.g. "3 days ago". */
function formatDeletedAgo(iso: string): string {
  const deletedMs = new Date(iso).getTime()
  const nowMs = Date.now()
  const diffMs = nowMs - deletedMs
  const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24))
  if (diffDays === 0) return 'today'
  if (diffDays === 1) return '1 day ago'
  if (diffDays < 14) return `${diffDays} days ago`
  return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}

/** Days remaining before auto-purge (30-day window). Clamped to [0, 30]. */
function daysRemaining(iso: string): number {
  const deletedMs = new Date(iso).getTime()
  const diffDays = Math.floor((Date.now() - deletedMs) / (1000 * 60 * 60 * 24))
  return Math.max(0, 30 - diffDays)
}

/** Secondary line rendered under a trash row title when deleted_at is present. */
function TrashMeta({ deletedAt }: { deletedAt?: string | null }) {
  if (!deletedAt) return null
  const ago = formatDeletedAgo(deletedAt)
  const left = daysRemaining(deletedAt)
  return (
    <span className="block text-xs text-muted-foreground">
      Deleted {ago} &middot; {left} {left === 1 ? 'day' : 'days'} left
    </span>
  )
}

export function TrashScreen() {
  const ctx = useOutletContext<Ctx | null>()
  const refresh = ctx?.refresh ?? (() => {})
  const { notify } = useToast()
  const [notes, setNotes] = useState<Note[] | null>(null)
  const [folders, setFolders] = useState<Folder[] | null>(null)
  const [lists, setLists] = useState<SmartList[] | null>(null)
  const [purgeId, setPurgeId] = useState<string | null>(null)
  const [purgeFolderId, setPurgeFolderId] = useState<string | null>(null)
  const [purgeListId, setPurgeListId] = useState<string | null>(null)

  const load = useCallback(async () => {
    try { setNotes(await muesli.listTrash()) }
    catch (err) { notify(err instanceof Error ? err.message : 'Could not load trash', 'error'); setNotes([]) }
    try { setFolders(await muesli.listTrashedFolders()) }
    catch (err) { notify(err instanceof Error ? err.message : 'Could not load trash', 'error'); setFolders([]) }
    try { setLists(await muesli.listTrashedSmartLists()) }
    catch (err) { notify(err instanceof Error ? err.message : 'Could not load trash', 'error'); setLists([]) }
  }, [notify])

  useEffect(() => { void load() }, [load])

  const onRestore = async (id: string) => {
    try { await muesli.restoreNote(id); await load(); refresh() }
    catch (err) { notify(err instanceof Error ? err.message : 'Could not restore note', 'error') }
  }
  const onPurge = async (id: string) => {
    try { await muesli.permanentDeleteNote(id); setPurgeId(null); await load(); refresh() }
    catch (err) { notify(err instanceof Error ? err.message : 'Could not delete note', 'error'); setPurgeId(null) }
  }
  const onRestoreFolder = async (id: string) => {
    try { await muesli.restoreFolder(id); await load(); refresh() }
    catch (err) { notify(err instanceof Error ? err.message : 'Could not restore folder', 'error') }
  }
  const onPurgeFolder = async (id: string) => {
    try { await muesli.permanentDeleteFolder(id); setPurgeFolderId(null); await load(); refresh() }
    catch (err) { notify(err instanceof Error ? err.message : 'Could not delete folder', 'error'); setPurgeFolderId(null) }
  }
  const onRestoreList = async (id: string) => {
    try { await muesli.restoreSmartList(id); await load(); refresh() }
    catch (err) { notify(err instanceof Error ? err.message : 'Could not restore smart list', 'error') }
  }
  const onPurgeList = async (id: string) => {
    try { await muesli.permanentDeleteSmartList(id); setPurgeListId(null); await load(); refresh() }
    catch (err) { notify(err instanceof Error ? err.message : 'Could not delete smart list', 'error'); setPurgeListId(null) }
  }

  // Treat null (not-yet-loaded) lists as loading.
  if (notes === null || folders === null || lists === null) {
    return (
      <div className="mx-auto max-w-3xl p-6">
        <div className="mb-4 h-7 w-40"><Skeleton className="h-full w-full" /></div>
        <div className="flex flex-col gap-2">
          {Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}
        </div>
      </div>
    )
  }

  const isEmpty = notes.length === 0 && folders.length === 0 && lists.length === 0

  return (
    <div className="mx-auto max-w-3xl p-6">
      <h1 className="mb-1 font-serif text-xl font-semibold">Trash</h1>
      <p className="mb-4 text-sm text-muted-foreground">Items are deleted forever after 30 days.</p>
      {isEmpty ? (
        <EmptyState title="Trash is empty" hint="Notes and folders you delete will appear here for 30 days." />
      ) : (
        <div className="flex flex-col gap-6">
          {lists.length > 0 && (
            <section>
              <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Smart lists</h2>
              <ul className="flex flex-col gap-2">
                {lists.map((l) => (
                  <li key={l.id} className="flex items-center justify-between rounded-[var(--radius)] border border-border px-3 py-2">
                    <span className="min-w-0 flex-1 truncate">
                      {l.name || 'Untitled list'}
                      <TrashMeta deletedAt={l.deleted_at} />
                    </span>
                    <span className="ml-2 flex shrink-0 gap-2">
                      <Button variant="secondary" size="sm" onClick={() => onRestoreList(l.id)}>Restore</Button>
                      <Button variant="destructive" size="sm" onClick={() => setPurgeListId(l.id)}>Delete forever</Button>
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          )}
          {folders.length > 0 && (
            <section>
              <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Folders</h2>
              <ul className="flex flex-col gap-2">
                {folders.map((f) => (
                  <li key={f.id} className="flex items-center justify-between rounded-[var(--radius)] border border-border px-3 py-2">
                    <span className="min-w-0 flex-1 truncate">
                      {f.name || 'Untitled folder'}
                      <TrashMeta deletedAt={f.deleted_at} />
                    </span>
                    <span className="ml-2 flex shrink-0 gap-2">
                      <Button variant="secondary" size="sm" onClick={() => onRestoreFolder(f.id)}>Restore</Button>
                      <Button variant="destructive" size="sm" onClick={() => setPurgeFolderId(f.id)}>Delete forever</Button>
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          )}
          {notes.length > 0 && (
            <section>
              <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Notes</h2>
              <ul className="flex flex-col gap-2">
                {notes.map((n) => (
                  <li key={n.id} className="flex items-center justify-between rounded-[var(--radius)] border border-border px-3 py-2">
                    <span className="min-w-0 flex-1 truncate">
                      {n.title || 'Untitled note'}
                      <TrashMeta deletedAt={n.deleted_at} />
                    </span>
                    <span className="ml-2 flex shrink-0 gap-2">
                      <Button variant="secondary" size="sm" onClick={() => onRestore(n.id)}>Restore</Button>
                      <Button variant="destructive" size="sm" onClick={() => setPurgeId(n.id)}>Delete forever</Button>
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      )}
      {purgeId && (
        <Dialog open onOpenChange={(o) => !o && setPurgeId(null)} title="Delete forever?">
          <p className="text-sm text-muted-foreground">This permanently deletes the note, its transcript, summaries, and stored recording. Any files you&apos;ve previously exported to disk are not affected — only the recording stored on the server is removed. This cannot be undone.</p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" size="sm" onClick={() => setPurgeId(null)}>Cancel</Button>
            <Button variant="destructive" size="sm" onClick={() => onPurge(purgeId)}>Delete forever</Button>
          </div>
        </Dialog>
      )}
      {purgeFolderId && (
        <Dialog open onOpenChange={(o) => !o && setPurgeFolderId(null)} title="Delete forever?">
          <p className="text-sm text-muted-foreground">This permanently deletes the folder and everything inside it. This cannot be undone.</p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" size="sm" onClick={() => setPurgeFolderId(null)}>Cancel</Button>
            <Button variant="destructive" size="sm" onClick={() => onPurgeFolder(purgeFolderId)}>Delete forever</Button>
          </div>
        </Dialog>
      )}
      {purgeListId && (
        <Dialog open onOpenChange={(o) => !o && setPurgeListId(null)} title="Delete forever?">
          <p className="text-sm text-muted-foreground">This permanently deletes the smart list. This cannot be undone.</p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" size="sm" onClick={() => setPurgeListId(null)}>Cancel</Button>
            <Button variant="destructive" size="sm" onClick={() => onPurgeList(purgeListId)}>Delete forever</Button>
          </div>
        </Dialog>
      )}
    </div>
  )
}
