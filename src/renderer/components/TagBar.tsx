import { useState } from 'react'
import { X } from 'lucide-react'

// TagBar shows the note's tags as chips and a type-to-add input with autocomplete
// from known tags. Add/remove are async callbacks supplied by the parent (which
// owns the note id → server calls and refresh).
export function TagBar({
  tags,
  suggestions,
  onAdd,
  onRemove,
}: {
  tags: string[]
  suggestions: string[]
  onAdd: (name: string) => Promise<void>
  onRemove: (name: string) => Promise<void>
}) {
  const [value, setValue] = useState('')

  async function commit() {
    const name = value.trim()
    setValue('')
    if (!name) return
    if (tags.some((t) => t.toLowerCase() === name.toLowerCase())) return
    await onAdd(name)
  }

  const listId = 'tagbar-suggestions'
  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-border px-6 py-2">
      {tags.map((t) => (
        <span
          key={t}
          className="inline-flex items-center gap-1 rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary"
        >
          #{t}
          <button type="button" aria-label={`Remove ${t}`} onClick={() => onRemove(t)} className="rounded-full hover:bg-primary/20 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
            <X size={12} />
          </button>
        </span>
      ))}
      <input
        aria-label="Add tag"
        list={listId}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            void commit()
          }
        }}
        placeholder="Add tag…"
        className="h-6 min-w-24 flex-1 rounded bg-transparent text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring placeholder:text-muted-foreground"
      />
      <datalist id={listId}>
        {suggestions.map((s) => (
          <option key={s} value={s} />
        ))}
      </datalist>
    </div>
  )
}
