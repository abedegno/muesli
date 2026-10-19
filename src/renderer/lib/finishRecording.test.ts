import { expect, it, vi } from 'vitest'
import { finishRecordingAfterNavigation } from './finishRecording'
import { makeSilentWav } from '../../../e2e/helpers/wav'
import type { FullNote } from '../../shared/types'

function fixture() {
  const audio = makeSilentWav(1)
  const full: FullNote = {
    note: {
      id: 'owned-note',
      title: 'Owned recording',
      status: 'ready',
      created_at: '',
      updated_at: '',
      partial_transcript: false,
    },
    body_markdown: '',
    transcript: null,
    summaries: [],
  }
  return {
    audio,
    session: {
      stop: vi.fn().mockResolvedValue({
        bytes: audio,
        mimeType: 'audio/wav',
        hasMicAudio: true,
        hasSystemAudio: false,
        durationMs: 1000,
      }),
    },
    noteId: 'owned-note',
    title: 'Owned recording',
    bridge: {
      stopNoteCapture: vi.fn().mockResolvedValue(full.note),
      stopNoteStream: vi.fn().mockResolvedValue(undefined),
      uploadAudio: vi.fn().mockResolvedValue({ noteId: 'owned-note' }),
      getFull: vi.fn().mockResolvedValue(full),
      getRecordingRecovery: vi.fn().mockResolvedValue(null),
    },
    addUpload: vi.fn(),
    updateUpload: vi.fn(),
    addProcessing: vi.fn(),
    updateProcessing: vi.fn(),
    notify: vi.fn(),
    refresh: vi.fn(),
  }
}
it('stops and uploads the owned note after navigation, keeping activity attached to its identity', async () => {
  const options = fixture()
  await finishRecordingAfterNavigation(options)
  expect(options.bridge.stopNoteCapture).toHaveBeenCalledWith('owned-note')
  expect(options.bridge.uploadAudio).toHaveBeenCalledWith({
    noteId: 'owned-note',
    audio: options.audio.buffer,
    audioMimeType: 'audio/wav',
  })
  expect(options.updateUpload).toHaveBeenCalledWith('owned-note', 'done', true)
  expect(options.updateProcessing).toHaveBeenCalledWith('owned-note', 'ready', true)
})
it('waits for a pending start-marker request before clearing the recording marker', async () => {
  const options = fixture()
  let started!: () => void
  const beforeStopMarker = new Promise<void>((resolve) => {
    started = resolve
  })
  const finished = finishRecordingAfterNavigation({ ...options, beforeStopMarker })
  await Promise.resolve()
  expect(options.bridge.stopNoteCapture).not.toHaveBeenCalled()
  started()
  await finished
  expect(options.bridge.stopNoteCapture).toHaveBeenCalledTimes(1)
})
it('points to the saved recovery when upload fails, without claiming successful processing', async () => {
  const options = fixture()
  options.bridge.uploadAudio.mockRejectedValue(new Error('offline'))
  options.bridge.getRecordingRecovery.mockResolvedValue({
    noteId: 'owned-note',
    byteLength: options.audio.length,
    mimeType: 'audio/wav',
    createdAt: '',
  })
  await finishRecordingAfterNavigation(options)
  expect(options.updateUpload).toHaveBeenCalledWith('owned-note', 'error', true)
  expect(options.notify).toHaveBeenCalledWith(
    expect.stringContaining('saved on this device'),
    'error'
  )
  expect(options.addProcessing).not.toHaveBeenCalled()
})
it('keeps upload success separate from a later processing-status failure', async () => {
  const options = fixture()
  options.bridge.getFull.mockRejectedValue(new Error('offline'))
  await finishRecordingAfterNavigation(options)
  expect(options.updateUpload).not.toHaveBeenCalledWith('owned-note', 'error', true)
  expect(options.notify).toHaveBeenCalledWith(
    expect.stringContaining('processing could not be checked'),
    'info'
  )
})
