// Chat-specific error classification. ipcHandlers.ts encodes the server's HTTP
// status into the rejected Error's message as a `[NNN] ` prefix (Electron's
// ipcMain.handle rejection path only round-trips `message`, dropping custom
// properties like ApiError.status — see ipcHandlers.ts withApiError). This
// module recovers that status so the UI can tell a 409 "already sending"
// race apart from any other (400/404/500) failure.
export type ChatErrorKind = 'inflight' | 'no-agent' | 'generic'

export interface ChatError {
  kind: ChatErrorKind
  message: string
}

const STATUS_PREFIX = /^\[(\d+)\]\s*(.*)$/

export function parseChatError(err: unknown): ChatError {
  const raw = err instanceof Error ? err.message : 'Something went wrong. Please try again.'
  const m = STATUS_PREFIX.exec(raw)
  if (m && m[1] === '409') {
    return { kind: 'inflight', message: 'A message is already sending, please wait…' }
  }
  if (m && m[1] === '422') {
    return {
      kind: 'no-agent',
      message: CHAT_UNAVAILABLE_MESSAGE,
    }
  }
  // 400/404/500 (missing/misconfigured plugin, plugin-call failure, etc.) all
  // surface as a generic, retryable error — never crash the thread view.
  return { kind: 'generic', message: (m ? m[2] : raw) || 'Something went wrong. Please try again.' }
}
import { CHAT_UNAVAILABLE_MESSAGE } from '../AgentUnavailableNotice'
