import { errorStatus } from '@/lib/apiError'

import { CHAT_UNAVAILABLE_MESSAGE } from '../AgentUnavailableNotice'

// Chat-specific error classification. The renderer bridge recovers the HTTP
// status that ipcHandlers.ts encodes into Electron's rejected Error message.
// Reading that structural status keeps chat classification aligned with the
// normalised BridgeError that reaches components.
// 'stale-selection' (issue #765's cross-meeting analysis) is a DEDICATED kind
// for HTTP 412: a selected meeting's transcript changed between the
// renderer's selection and the server's immediately-pre-invocation check.
// It needs its own kind (not folded into 'generic') because Electron IPC
// preserves the numeric status code but not arbitrary response-body
// discriminator fields, so 412 is the only signal available to distinguish
// "retry with a fresh selection" from any other failure.
export type ChatErrorKind = 'inflight' | 'no-agent' | 'stale-selection' | 'generic'

export interface ChatError {
  kind: ChatErrorKind
  message: string
}

export function parseChatError(err: unknown): ChatError {
  const status = errorStatus(err)
  if (status === 409) {
    return { kind: 'inflight', message: 'A message is already sending, please wait…' }
  }
  if (status === 412) {
    return {
      kind: 'stale-selection',
      message: 'One or more selected meetings changed since you selected them. Please retry.',
    }
  }
  if (status === 422) {
    return {
      kind: 'no-agent',
      message: CHAT_UNAVAILABLE_MESSAGE,
    }
  }
  // 400/404/413/500 (malformed input, missing/misconfigured plugin, note-count
  // or context-budget limit exceeded, plugin-call failure, etc.) all surface
  // as a generic, retryable error using the server's own message text —
  // never crash the thread view. 413 deliberately stays generic (no
  // dedicated kind): the server's message text is already specific enough,
  // and unlike 412 it carries no retry-with-fresh-selection semantics.
  const message = err instanceof Error ? err.message : ''
  return { kind: 'generic', message: message || 'Something went wrong. Please try again.' }
}
