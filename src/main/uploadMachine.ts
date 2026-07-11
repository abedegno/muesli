import type { MuesliClient } from './muesliClient'

type UploadPhase =
  | 'requesting-url'
  | 'uploading-audio'
  | 'confirming-upload'
  | 'done'
  | 'error'

export interface UploadProgress {
  phase: UploadPhase
  noteId?: string
  error?: string
}

interface UploadResult {
  noteId: string
}

type ProgressFn = (p: UploadProgress) => void

interface UploadAudioInput {
  noteId: string
  audio: Uint8Array | null
  audioMimeType?: string
}

// uploadAudioToNote uploads audio for an ALREADY-CREATED note: presign → PUT →
// confirm. Used by the live-notes flow where the note exists before recording.
export async function uploadAudioToNote(
  client: MuesliClient,
  input: UploadAudioInput,
  onProgress: ProgressFn = () => {},
): Promise<UploadResult> {
  const { noteId } = input
  try {
    if (input.audio && input.audio.length > 0) {
      onProgress({ phase: 'requesting-url', noteId })
      const grant = await client.getAudioUploadUrl(noteId)
      onProgress({ phase: 'uploading-audio', noteId })
      await client.putAudio(grant, input.audio, input.audioMimeType)
      onProgress({ phase: 'confirming-upload', noteId })
      await client.markAudioUploaded(noteId, grant.key)
    }
    onProgress({ phase: 'done', noteId })
    return { noteId }
  } catch (err) {
    onProgress({ phase: 'error', noteId, error: err instanceof Error ? err.message : String(err) })
    throw err
  }
}
