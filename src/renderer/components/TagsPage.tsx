import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { Dialog } from '@/components/ui/Dialog'
import { useToast } from '@/components/ui/Toast'
import { TagRenameDialog } from './TagRenameDialog'

type Tag = { id: string; name: string; count: number }

export function TagsPage() {
  const [tags, setTags] = useState<Tag[] | null>(null)
  const [renamingTag, setRenamingTag] = useState<Tag | null>(null)
  const [confirmDeleteTag, setConfirmDeleteTag] = useState<Tag | null>(null)
  const navigate = useNavigate()
  const { notify } = useToast()

  const reload = () =>
    muesli.listTags().then((t) => setTags(t))

  useEffect(() => {
    reload()
  }, [])

  return (
    <div className="mx-auto max-w-xl p-8">
      <div className="mb-6 flex items-center gap-3">
        <Button variant="secondary" size="sm" onClick={() => navigate('/settings')}>
          ← Back to Settings
        </Button>
        <h1 className="text-xl font-semibold">Manage Tags</h1>
      </div>

      {tags === null ? (
        <p className="text-sm text-muted-foreground">Loading…</p>
      ) : tags.length === 0 ? (
        <p className="text-sm text-muted-foreground">No tags yet.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {tags.map((tag) => (
            <li
              key={tag.id}
              className="flex items-center justify-between rounded-[var(--radius)] border border-border p-3"
            >
              <div>
                <span className="font-medium">{tag.name}</span>
                <span className="ml-2 text-xs text-muted-foreground">
                  {tag.count} note{tag.count === 1 ? '' : 's'}
                </span>
              </div>
              <div className="flex gap-2">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => setRenamingTag(tag)}
                >
                  Rename
                </Button>
                <Button
                  variant="destructive"
                  size="sm"
                  aria-label={`Delete ${tag.name}`}
                  onClick={() => setConfirmDeleteTag(tag)}
                >
                  Delete
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}

      {renamingTag !== null && (
        <TagRenameDialog
          key={renamingTag.id}
          open
          initialName={renamingTag.name}
          onSave={async (name) => {
            try {
              await muesli.renameTag(renamingTag.id, name)
              await reload()
            } catch (err) {
              notify(err instanceof Error ? err.message : 'Could not rename tag', 'error')
              throw err
            }
          }}
          onClose={() => setRenamingTag(null)}
        />
      )}

      {confirmDeleteTag !== null && (
        <Dialog
          open
          onOpenChange={(o) => {
            if (!o) setConfirmDeleteTag(null)
          }}
          title="Delete tag?"
        >
          <p className="text-sm text-muted-foreground">
            Delete "{confirmDeleteTag.name}"? This removes it from {confirmDeleteTag.count} note
            {confirmDeleteTag.count === 1 ? '' : 's'} and cannot be undone.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" size="sm" onClick={() => setConfirmDeleteTag(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={async () => {
                try {
                  await muesli.deleteTag(confirmDeleteTag.id)
                  await reload()
                  setConfirmDeleteTag(null)
                } catch (err) {
                  notify(err instanceof Error ? err.message : 'Could not delete tag', 'error')
                  setConfirmDeleteTag(null)
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
