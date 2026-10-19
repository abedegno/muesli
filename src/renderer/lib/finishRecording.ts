import { uiText } from '@/i18n'
import type { RecordingSession } from '../../main/recorder'
import type { MuesliBridge } from '../../shared/ipc'
import type { NoteStatus } from '../../shared/types'
import { isTerminal } from '../../shared/types'
import { pollNote } from './pollNote'

type Bridge = Pick<
  MuesliBridge,
  'stopNoteCapture' | 'stopNoteStream' | 'uploadAudio' | 'getFull' | 'getRecordingRecovery'
> &
  Partial<Pick<MuesliBridge, 'endRecordingJournal'>>
interface Options {
  session: Pick<RecordingSession, 'stop'>
  noteId: string
  beforeStopMarker?: Promise<unknown>
  captureId?: string
  title: string
  bridge: Bridge
  addUpload: (id: string, title: string) => void
  updateUpload: (id: string, phase: 'done' | 'error', done: boolean) => void
  addProcessing: (id: string, title: string, status: NoteStatus) => void
  updateProcessing: (id: string, status: NoteStatus, done: boolean) => void
  notify: (message: string, kind: 'info' | 'error') => void
  refresh: () => void
}

export async function trackProcessingAfterNavigation(
  options: Omit<Options, 'session' | 'addUpload' | 'updateUpload' | 'addProcessing'>
): Promise<void> {
  try {
    await pollNote(options.noteId, options.bridge.getFull, {
      maxAttempts: 1200,
      onUpdate: (full) => {
        options.updateProcessing(options.noteId, full.note.status, isTerminal(full.note.status))
        options.refresh()
      },
    })
  } catch {
    options.notify(
      uiText(
        'Your audio is saved, but processing could not be checked. Open the note to check its progress.'
      ),
      'info'
    )
  }
}

// Leaving a note ends that note's owned capture. Finish/upload independently
// of its React component; completion must never navigate the user back to it.
export async function finishRecordingAfterNavigation(options: Options): Promise<void> {
  const { session, noteId, title, bridge, refresh } = options
  let uploaded = false
  try {
    const result = await session.stop()
    if (options.captureId) await bridge.endRecordingJournal?.(options.captureId).catch(() => {})
    await bridge.stopNoteStream?.(noteId).catch(() => {})
    await options.beforeStopMarker?.catch(() => {})
    await (
      options.captureId
        ? bridge.stopNoteCapture?.(noteId, options.captureId)
        : bridge.stopNoteCapture?.(noteId)
    ).catch(() => {})
    refresh()
    if (!result.bytes.length)
      throw new Error('No audio was captured. Check your microphone before recording again.')
    options.addUpload(noteId, title)
    await bridge.uploadAudio({
      noteId,
      ...(options.captureId ? { captureId: options.captureId } : {}),
      audio: result.bytes.slice().buffer as ArrayBuffer,
      audioMimeType: result.mimeType,
    })
    uploaded = true
    options.updateUpload(noteId, 'done', true)
    options.addProcessing(noteId, title, 'uploaded')
    options.notify(uiText('Recording stopped and saved when you left the note.'), 'info')
    await trackProcessingAfterNavigation(options)
  } catch (error) {
    if (!uploaded) options.updateUpload(noteId, 'error', true)
    const backup = await bridge.getRecordingRecovery?.(noteId).catch(() => null)
    options.notify(
      backup
        ? uiText('Your recording is saved on this device. Open its note to retry the upload.')
        : error instanceof Error
          ? error.message
          : uiText('Could not finish recording.'),
      'error'
    )
  }
}
