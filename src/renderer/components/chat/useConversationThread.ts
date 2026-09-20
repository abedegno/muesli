import { useCallback, useEffect, useRef, useState } from 'react'
import { muesli } from '@/api'
import type { ChatSource, Conversation, Message, Template } from '../../../shared/types'
import type { CrossAnalysisRequest } from '../../../shared/ipc'
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

  // sendCrossAnalysis submits issue #765's cross-meeting analysis: an
  // explicit ordered note-id list, a template id, and an optional focus.
  // Reuses the SAME sending/error state and create-and-send-vs-existing
  // branching as ordinary send(), so the conversation's existing in-flight
  // guard, error area, and navigation continue to apply unchanged.
  const sendCrossAnalysis = useCallback(
    async (templateId: string, noteIds: string[], focus: string, templateName: string): Promise<boolean> => {
      if (sending) return false
      setError(null)
      setSending(true)
      const crossAnalysis: CrossAnalysisRequest = { template_id: templateId, note_ids: noteIds }
      // Optimistic, server-equivalent descriptor: the same focus-plus-
      // statement shape the server persists as the user turn, computed
      // client-side so the bubble is not simply "Cross-meeting analysis" --
      // it is removed/replaced the moment a real response (or an error)
      // arrives.
      const statement = `Cross-meeting analysis using template "${templateName}" across ${noteIds.length} meetings.`
      const optimisticContent = focus ? `${focus}

${statement}` : statement
      const optimisticId = `local-${Date.now()}-${Math.random().toString(36).slice(2)}`
      const optimisticMessage: Message = {
        id: optimisticId,
        conversation_id: conversationId ?? '',
        role: 'user',
        content: optimisticContent,
        model: '',
        created_at: new Date().toISOString(),
      }
      setMessages((prev) => [...prev, optimisticMessage])
      try {
        if (!conversationId) {
          const res = await muesli.createConversation({ title: '', content: focus, cross_analysis: crossAnalysis })
          const { message, ...conversation } = res
          loadedIdRef.current = conversation.id
          onConversationCreated?.(conversation as Conversation)
          setMessages((prev) => {
            const withoutOptimistic = prev.filter((m) => m.id !== optimisticId)
            const userMessage: Message = { ...optimisticMessage, conversation_id: conversation.id }
            return message ? [...withoutOptimistic, userMessage, message] : [...withoutOptimistic, userMessage]
          })
        } else {
          const res = await muesli.sendMessage(conversationId, { content: focus, cross_analysis: crossAnalysis })
          setMessages((prev) => [...prev.filter((m) => m.id !== optimisticId), { ...optimisticMessage, conversation_id: conversationId }, res.message])
        }
        return true
      } catch (err) {
        // Roll back the optimistic bubble -- neither path persisted it
        // server-side on failure -- and use the existing chat error area.
        setMessages((prev) => prev.filter((m) => m.id !== optimisticId))
        setError(parseChatError(err))
        return false
      } finally {
        setSending(false)
      }
    },
    [conversationId, onConversationCreated, sending],
  )

  return { messages, sourcesByMessageId, loading, sending, error, send, sendCrossAnalysis, setError }
}

// crossAnalysisTemplateName is a small helper so callers (CrossAnalysisComposer's
// host) can resolve a template's display name from its id without threading
// the whole template list through this hook.
export function crossAnalysisTemplateName(templates: Template[], templateId: string): string {
  return templates.find((t) => t.id === templateId)?.name ?? ''
}
