import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'

type Tag = { id: string; name: string; count: number }
type RenameState = { id: string; name: string } | null

export function TagsPage() {
  const [tags, setTags] = useState<Tag[] | null>(null)
  const [renaming, setRenaming] = useState<RenameState>(null)
  const [renameInput, setRenameInput] = useState('')
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  const reload = () =>
    muesli.listTags().then((t) => setTags(t))

  useEffect(() => {
    reload()
  }, [])

  async function submitRename(id: string) {
    const name = renameInput.trim()
    if (!name) return
    try {
      await muesli.renameTag(id, name)
      setRenaming(null)
      setError(null)
      await reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not rename tag')
    }
  }

  async function confirmDelete(id: string, name: string) {
    if (!window.confirm(`Delete tag "${name}"? It will be removed from all notes.`)) return
    try {
      await muesli.deleteTag(id)
      setError(null)
      await reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not delete tag')
    }
  }

  return (
    <div className="mx-auto max-w-xl p-8">
      <div className="mb-6 flex items-center gap-3">
        <Button variant="secondary" size="sm" onClick={() => navigate('/settings')}>
          ← Back to Settings
        </Button>
        <h1 className="text-xl font-semibold">Manage Tags</h1>
      </div>

      {error && (
        <p className="mb-4 rounded border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {error}
        </p>
      )}

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
              {renaming?.id === tag.id ? (
                <form
                  className="flex flex-1 items-center gap-2"
                  onSubmit={(e) => {
                    e.preventDefault()
                    void submitRename(tag.id)
                  }}
                >
                  <Input
                    aria-label="Tag name"
                    autoFocus
                    value={renameInput}
                    onChange={(e) => setRenameInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Escape') setRenaming(null)
                    }}
                    className="flex-1"
                  />
                  <Button size="sm" type="submit" disabled={!renameInput.trim()}>
                    Save
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    type="button"
                    onClick={() => setRenaming(null)}
                  >
                    Cancel
                  </Button>
                </form>
              ) : (
                <>
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
                      onClick={() => {
                        setRenaming({ id: tag.id, name: tag.name })
                        setRenameInput(tag.name)
                      }}
                    >
                      Rename
                    </Button>
                    <Button
                      variant="destructive"
                      size="sm"
                      aria-label={`Delete ${tag.name}`}
                      onClick={() => void confirmDelete(tag.id, tag.name)}
                    >
                      Delete
                    </Button>
                  </div>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
