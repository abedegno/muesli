import type { NoteStatus } from '../../shared/types'

const ALREADY_RECORDED_REASON = 'This note already has a recording'

export function recordUnavailableReason(status: NoteStatus): string | undefined {
  switch (status) {
    case 'draft':
    case 'recording':
      return undefined
    case 'uploaded':
    case 'transcribing':
    case 'summarizing':
    case 'ready':
      return ALREADY_RECORDED_REASON
    case 'failed':
      // Pipeline failures happen after upload; retrying, not re-recording, is the recovery path.
      return ALREADY_RECORDED_REASON
    default: {
      const _exhaustive: never = status
      return _exhaustive
    }
  }
}
