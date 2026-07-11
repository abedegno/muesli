import { useCallback, useEffect, useRef, useState } from 'react'
import { muesli } from '@/api'
import type { ChatSource, Conversation, Message } from '../../../shared/types'
import { parseChatError, type ChatError } from './chatErrors'

// Drives ONE conversation's message thread: loading its history, sending new
// messages (creating the conversation lazily on the first send when none
// exists yet — the "create-and-send" path), and tracking pending/error state.
// Shared by both entry points: the note-scoped panel (NoteChatPanel, passes
// `noteId`) and the global chat screen (ChatScreen, `noteId` omitted).
export function useConversationThread({
  noteId,
  conversationId,
  onConversationCreated,
}: {
  noteId?: string
  conversationId: string | null
  onConversationCreated?: (c: Conversation) => void
}) {
  const [messages, setMessages] = useState<Message[]>([])
  const [sourcesByMessageId, setSourcesByMessageId] = useState<Record<string, ChatSource[]>>({})
  const [loading, setLoading] = useState(false)
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<ChatError | null>(null)
  // The conversationId whose messages are already reflected in local state —
  // either from a prior fetch, or because send() just populated them itself
  // (create-and-send / sendMessage). Guards the load effect below from
  // clobbering that state with a redundant listMessages() re-fetch the moment
  // `conversationId` transitions from null to the newly-created id.
  const loadedIdRef = useRef<string | null>(null)

  useEffect(() => {
    setError(null)
    if (!conversationId) {
      setMessages([])
      setSourcesByMessageId({})
      loadedIdRef.current = null
      return
    }
    if (loadedIdRef.current === conversationId) return
    let cancelled = false
    setLoading(true)
    muesli
      .listMessages(conversationId)
      .then((msgs) => {
        if (cancelled) return
        setMessages(msgs)
        setSourcesByMessageId({})
        loadedIdRef.current = conversationId
      })
      .catch((err) => {
        if (!cancelled) setError(parseChatError(err))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [conversationId])

  const send = useCallback(
    async (content: string): Promise<boolean> => {
      if (sending) return false
      setError(null)
      setSending(true)
      const optimisticId = `local-${Date.now()}-${Math.random().toString(36).slice(2)}`
      const optimisticMessage: Message = {
        id: optimisticId,
        conversation_id: conversationId ?? '',
        role: 'user',
        content,
        model: '',
        created_at: new Date().toISOString(),
      }
      setMessages((prev) => [...prev, optimisticMessage])
      try {
        if (!conversationId) {
          const title = content.length > 60 ? `${content.slice(0, 57)}…` : content
          const res = await muesli.createConversation({ note_id: noteId, title, content })
          const { message, sources, ...conversation } = res
          // Mark this id as already-loaded BEFORE flipping conversationId (via
          // onConversationCreated), so the load effect's re-render doesn't
          // stomp the messages we're about to set with a redundant re-fetch.
          loadedIdRef.current = conversation.id
          onConversationCreated?.(conversation as Conversation)
          setMessages((prev) => {
            const withoutOptimistic = prev.filter((m) => m.id !== optimisticId)
            const userMessage: Message = { ...optimisticMessage, conversation_id: conversation.id }
            return message ? [...withoutOptimistic, userMessage, message] : [...withoutOptimistic, userMessage]
          })
          if (message && sources && sources.length > 0) {
            setSourcesByMessageId((prev) => ({ ...prev, [message.id]: sources }))
          }
        } else {
          const res = await muesli.sendMessage(conversationId, { content })
          setMessages((prev) => [...prev, res.message])
          if (res.sources && res.sources.length > 0) {
            setSourcesByMessageId((prev) => ({ ...prev, [res.message.id]: res.sources }))
          }
        }
        return true
      } catch (err) {
        // Roll back the optimistic bubble — neither path persisted it server-side,
        // so leaving it in place would misrepresent the conversation state.
        setMessages((prev) => prev.filter((m) => m.id !== optimisticId))
        setError(parseChatError(err))
        return false
      } finally {
        setSending(false)
      }
    },
    [conversationId, noteId, onConversationCreated, sending],
  )

  return { messages, sourcesByMessageId, loading, sending, error, send, setError }
}
