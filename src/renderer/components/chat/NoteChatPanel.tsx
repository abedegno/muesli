import { useEffect, useState } from 'react'
import { X } from 'lucide-react'
import { muesli } from '@/api'
import { cn } from '@/lib/cn'
import type { ChatSource, Conversation } from '../../../shared/types'
import { ChatComposer } from './ChatComposer'
import { ChatThread } from './ChatThread'
import { useConversationThread } from './useConversationThread'

// Entry point (a): "Ask about this note". Resolves (or lazily creates, on
// first send) the note-scoped conversation for `noteId` and shows its thread.
export function NoteChatPanel({
  noteId,
  onClose,
  onCiteClick,
}: {
  noteId: string
  onClose: () => void
  // Invoked when an inline citation chip is clicked. This panel is always
  // note-scoped (chatSources restricts retrieval to `noteId`'s own
  // transcript -- see internal/api/chat.go's chatSources), so a click never
  // needs to navigate to a different note: the caller (NoteView) can just
  // jump straight to the cited segment in its own already-open transcript.
  onCiteClick?: (source: ChatSource) => void
}) {
  const [conversation, setConversation] = useState<Conversation | null>(null)
  const [initLoading, setInitLoading] = useState(true)
  const [initError, setInitError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setInitLoading(true)
    setInitError(null)
    setConversation(null)
    muesli
      .listConversations(noteId)
      .then((list) => {
        if (cancelled) return
        // Most-recently-updated conversation for this note, if any already exist.
        const sorted = [...list].sort((a, b) => (a.updated_at < b.updated_at ? 1 : -1))
        setConversation(sorted[0] ?? null)
      })
      .catch((err) => {
        if (!cancelled) setInitError(err instanceof Error ? err.message : 'Could not load conversation.')
      })
      .finally(() => {
        if (!cancelled) setInitLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [noteId])

  const thread = useConversationThread({
    noteId,
    conversationId: conversation?.id ?? null,
    onConversationCreated: setConversation,
  })

  return (
    <div className="flex h-full flex-col">
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-sm font-semibold">Ask about this note</h3>
        <button
          type="button"
          aria-label="Close chat"
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground"
        >
          <X size={16} />
        </button>
      </div>

      {initLoading ? (
        <p className="text-sm text-muted-foreground">Loading conversation…</p>
      ) : initError ? (
        <p role="alert" className="text-sm text-destructive">{initError}</p>
      ) : (
        <ChatThread
          messages={thread.messages}
          sourcesByMessageId={thread.sourcesByMessageId}
          loading={thread.loading}
          emptyLabel="Ask a question about this note to get started."
          onCiteClick={onCiteClick}
        />
      )}

      {thread.error && (
        <p
          role="alert"
          className={cn(
            'mb-1 text-xs',
            thread.error.kind === 'inflight' || thread.error.kind === 'no-agent' ? 'text-muted-foreground' : 'text-destructive',
          )}
        >
          {thread.error.message}
        </p>
      )}

      <ChatComposer sending={thread.sending} onSend={thread.send} />
    </div>
  )
}
