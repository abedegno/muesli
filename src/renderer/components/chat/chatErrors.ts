import { errorStatus } from '@/lib/apiError'

import { CHAT_UNAVAILABLE_MESSAGE } from '../AgentUnavailableNotice'

// Chat-specific error classification. The renderer bridge recovers the HTTP
// status that ipcHandlers.ts encodes into Electron's rejected Error message.
// Reading that structural status keeps chat classification aligned with the
// normalised BridgeError that reaches components.
export type ChatErrorKind = 'inflight' | 'no-agent' | 'generic'

export interface ChatError {
  kind: ChatErrorKind
  message: string
}

export function parseChatError(err: unknown): ChatError {
  const status = errorStatus(err)
  if (status === 409) {
    return { kind: 'inflight', message: 'A message is already sending, please wait…' }
  }
  if (status === 422) {
    return {
      kind: 'no-agent',
      message: CHAT_UNAVAILABLE_MESSAGE,
    }
  }
  // 400/404/500 (missing/misconfigured plugin, plugin-call failure, etc.) all
  // surface as a generic, retryable error — never crash the thread view.
  const message = err instanceof Error ? err.message : ''
  return { kind: 'generic', message: message || 'Something went wrong. Please try again.' }
}
