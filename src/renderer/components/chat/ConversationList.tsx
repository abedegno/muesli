import { Plus } from 'lucide-react'
import { cn } from '@/lib/cn'
import type { Conversation } from '../../../shared/types'

// Titles list for the global/cross-note chat entry point. Selecting a
// conversation loads its thread; "New conversation" clears the selection so
// the next sent message starts a fresh (unsaved) conversation.
export function ConversationList({
  conversations,
  selectedId,
  onSelect,
  onNew,
}: {
  conversations: Conversation[]
  selectedId: string | null
  onSelect: (id: string) => void
  onNew: () => void
}) {
  return (
    <div className="flex w-64 shrink-0 flex-col border-r border-border pr-3">
      <div className="mb-2 flex items-center justify-between">
        <h2 className="text-sm font-semibold">Conversations</h2>
        <button
          type="button"
          aria-label="New conversation"
          onClick={onNew}
          className="rounded-[var(--radius)] p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <Plus size={16} />
        </button>
      </div>
      {conversations.length === 0 ? (
        <p className="px-1 text-xs text-muted-foreground">No conversations yet.</p>
      ) : (
        <ul className="flex flex-col gap-0.5 overflow-y-auto">
          {conversations.map((c) => (
            <li key={c.id}>
              <button
                type="button"
                aria-current={c.id === selectedId ? 'true' : undefined}
                onClick={() => onSelect(c.id)}
                className={cn(
                  'block w-full truncate rounded-[var(--radius)] px-2 py-1.5 text-left text-sm',
                  c.id === selectedId ? 'bg-primary/10 font-medium text-primary' : 'text-foreground hover:bg-muted',
                )}
              >
                {c.title || 'Untitled conversation'}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
