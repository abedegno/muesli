import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { muesli } from '@/api'
import { cn } from '@/lib/cn'
import type { ChatSource, Conversation, MessageSource, Note, Template } from '../../../shared/types'
import { ChatComposer } from './ChatComposer'
import { ChatThread } from './ChatThread'
import { ConversationList } from './ConversationList'
import { CrossAnalysisComposer } from './CrossAnalysisComposer'
import { crossAnalysisTemplateName, useConversationThread } from './useConversationThread'

type ChatMode = 'chat' | 'cross-analysis'

// Entry point (b): the global "Chat" sidebar item — a cross-note conversation
// list plus a thread view (no note_id). Lets the user pick an existing
// conversation or start a new global one. Also the ONLY entry point that
// gains issue #765's cross-meeting analysis mode -- note-scoped chat
// (NoteChatPanel) never does.
export function ChatScreen() {
  const navigate = useNavigate()
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [listLoading, setListLoading] = useState(true)
  const [listError, setListError] = useState<string | null>(null)
  const [mode, setMode] = useState<ChatMode>('chat')
  const [templates, setTemplates] = useState<Template[]>([])
  const [notes, setNotes] = useState<Note[]>([])

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

  // Templates/notes for the cross-analysis composer are fetched once the
  // mode is entered (not on every render) -- cheap and keeps ordinary chat's
  // initial load unaffected when the mode is never used.
  useEffect(() => {
    if (mode !== 'cross-analysis') return
    muesli.listTemplates().then(setTemplates).catch(() => setTemplates([]))
    muesli.listNotes().then(setNotes).catch(() => setNotes([]))
  }, [mode])

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

  // Cross-meeting analysis citations navigate the same way, once note_id is
  // known non-null (ChatThread already filters null note_ids to an inert,
  // non-clickable chip -- see renderTextWithCitations).
  const onCiteMessageSourceClick = useCallback(
    (source: MessageSource & { note_id: string }) => {
      navigate(`/notes/${source.note_id}?segment_index=${source.segment_index}`)
    },
    [navigate],
  )

  const handleCrossAnalysisSend = useCallback(
    (templateId: string, noteIds: string[], focus: string) =>
      thread.sendCrossAnalysis(templateId, noteIds, focus, crossAnalysisTemplateName(templates, templateId)),
    [thread, templates],
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
        <div className="mb-2 flex items-center justify-between">
          <h1 className="font-serif text-xl font-semibold">Chat</h1>
          <div role="tablist" aria-label="Chat mode" className="flex gap-1 text-xs">
            <button
              type="button"
              role="tab"
              aria-selected={mode === 'chat'}
              onClick={() => setMode('chat')}
              className={cn('rounded-[var(--radius)] px-2 py-1', mode === 'chat' ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground')}
            >
              Chat
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={mode === 'cross-analysis'}
              onClick={() => setMode('cross-analysis')}
              className={cn(
                'rounded-[var(--radius)] px-2 py-1',
                mode === 'cross-analysis' ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground',
              )}
            >
              Cross-meeting analysis
            </button>
          </div>
        </div>
        {listLoading && <p className="text-sm text-muted-foreground">Loading conversations…</p>}
        {listError && (
          <p role="alert" className="text-sm text-destructive">
            {listError}
          </p>
        )}
        <ChatThread
          messages={thread.messages}
          sourcesByMessageId={thread.sourcesByMessageId}
          loading={thread.loading}
          emptyLabel={selectedId ? undefined : 'Start a new conversation by asking a question below.'}
          onCiteClick={onCiteClick}
          onCiteMessageSourceClick={onCiteMessageSourceClick}
        />
        {thread.error && (
          <p
            role="alert"
            className={cn(
              'mb-1 text-xs',
              thread.error.kind === 'inflight' || thread.error.kind === 'no-agent' || thread.error.kind === 'stale-selection'
                ? 'text-muted-foreground'
                : 'text-destructive',
            )}
          >
            {thread.error.message}
          </p>
        )}
        {mode === 'chat' ? (
          <ChatComposer sending={thread.sending} onSend={thread.send} />
        ) : (
          <CrossAnalysisComposer templates={templates} notes={notes} sending={thread.sending} onSend={handleCrossAnalysisSend} />
        )}
      </div>
    </div>
  )
}
