import type { NoteStatus } from '../../shared/types'

export function statusLabel(s: NoteStatus): string {
  switch (s) {
    case 'recording':
      return 'Recording'
    case 'uploaded':
      return 'Uploaded'
    case 'transcribing':
      return 'Transcribing'
    case 'summarizing':
      return 'Summarizing'
    case 'ready':
      return 'Ready'
    case 'failed':
      return 'Failed'
  }
}

export function statusTone(s: NoteStatus): 'neutral' | 'primary' | 'accent' | 'destructive' {
  if (s === 'ready') return 'primary'
  if (s === 'failed') return 'destructive'
  if (s === 'transcribing' || s === 'summarizing' || s === 'uploaded') return 'accent'
  return 'neutral'
}

export function isProcessing(s: NoteStatus): boolean {
  return s === 'uploaded' || s === 'transcribing' || s === 'summarizing'
}
