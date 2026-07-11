import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { muesli } from '@/api'
import { cn } from '@/lib/cn'
import type { ChatSource, Conversation } from '../../../shared/types'
import { ChatComposer } from './ChatComposer'
import { ChatThread } from './ChatThread'
import { ConversationList } from './ConversationList'
import { useConversationThread } from './useConversationThread'

// Entry point (b): the global "Chat" sidebar item — a cross-note conversation
// list plus a thread view (no note_id). Lets the user pick an existing
// conversation or start a new global one.
export function ChatScreen() {
  const navigate = useNavigate()
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [listLoading, setListLoading] = useState(true)
  const [listError, setListError] = useState<string | null>(null)

  const refreshList = useCallback(() => {
    setListLoading(true)
    setListError(null)
    muesli
      .listConversations()
      .then((list) => setConversations(list))
      .catch((err) => setListError(err instanceof Error ? err.message : 'Could not load conversations.'))
      .finally(() => setListLoading(false))
  }, [])

  useEffect(() => {
    refreshList()
  }, [refreshList])

  const thread = useConversationThread({
    conversationId: selectedId,
    onConversationCreated: (c) => {
      setSelectedId(c.id)
      setConversations((prev) => [c, ...prev.filter((p) => p.id !== c.id)])
    },
  })

  // Global chat has no single "current note" (a conversation's sources can
  // point at any of the owner's notes -- see chatSources' cross-note TopK
  // path), so a citation click needs a real route navigation: jump to the
  // cited note and ask it to scroll to + highlight the cited segment via the
  // same `?segment_index=` convention NoteView's own jumpToCitation uses.
  const onCiteClick = useCallback(
    (source: ChatSource) => {
      navigate(`/notes/${source.note_id}?segment_index=${source.segment_index}`)
    },
    [navigate],
  )

  return (
    <div className="flex h-full gap-4 p-6">
      <ConversationList
        conversations={conversations}
        selectedId={selectedId}
        onSelect={setSelectedId}
        onNew={() => setSelectedId(null)}
      />
      <div className="flex min-w-0 flex-1 flex-col">
        <h1 className="mb-2 font-serif text-xl font-semibold">Chat</h1>
        {listLoading && <p className="text-sm text-muted-foreground">Loading conversations…</p>}
        {listError && <p role="alert" className="text-sm text-destructive">{listError}</p>}
        <ChatThread
          messages={thread.messages}
          sourcesByMessageId={thread.sourcesByMessageId}
          loading={thread.loading}
          emptyLabel={selectedId ? undefined : 'Start a new conversation by asking a question below.'}
          onCiteClick={onCiteClick}
        />
        {thread.error && (
          <p
            role="alert"
            className={cn('mb-1 text-xs', thread.error.kind === 'inflight' ? 'text-muted-foreground' : 'text-destructive')}
          >
            {thread.error.message}
          </p>
        )}
        <ChatComposer sending={thread.sending} onSend={thread.send} />
      </div>
    </div>
  )
}
