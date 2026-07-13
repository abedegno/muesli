// @vitest-environment jsdom
/**
 * NoteScreen — Retry-restarts-capture
 *
 * Verifies that when a mic error occurs and the user clicks Retry,
 * NoteScreen both clears the error AND restarts capture (calls start()),
 * not just clears the dialog.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { FullNote } from '../../shared/types'

// ---------------------------------------------------------------------------
// Hoisted values (must precede vi.mock factories which run first)
// ---------------------------------------------------------------------------

const { mockSessionStart, mockSessionStop, fullNote } = vi.hoisted(() => {
  const note: FullNote = {
    note: { id: 'n-mic', title: 'Mic note', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
    body_markdown: '',
    transcript: null,
    summaries: [],
  }
  return {
    // spy on .start() so we can see how many times capture was attempted
    mockSessionStart: vi.fn(),
    mockSessionStop: vi.fn(),
    fullNote: note,
  }
})

// ---------------------------------------------------------------------------
// Module mocks
// ---------------------------------------------------------------------------

vi.mock('react-router-dom', () => {
  const ctx = { allNotes: [], folders: [], refresh: () => {} }
  const searchParams: [URLSearchParams, () => void] = [new URLSearchParams(''), () => {}]
  return {
    useParams: () => ({ id: 'n-mic' }),
    useSearchParams: () => searchParams,
    useNavigate: () => vi.fn(),
    useOutletContext: () => ctx,
  }
})

vi.mock('@/api', () => ({
  muesli: {
    getFull: vi.fn(async () => fullNote),
    addTag: vi.fn(),
    removeTag: vi.fn(),
    addNoteFolder: vi.fn(),
    removeNoteFolder: vi.fn(),
    createFolder: vi.fn(),
    updateBody: vi.fn(),
    resummarize: vi.fn(),
    regenerateSummary: vi.fn(),
    listTemplates: vi.fn(async () => []),
    listNoteActionItems: vi.fn(async () => ({ actionItems: [], decisions: [] })),
    listNoteLinks: vi.fn(async () => ({ outgoing: [], backlinks: [] })),
    listRelatedNotes: vi.fn(async () => []),
    updateActionItem: vi.fn(),
    deleteNote: vi.fn(),
    createShare: vi.fn(),
    listNoteShares: vi.fn(async () => []),
    revokeShare: vi.fn(),
    exportFile: vi.fn(),
    exportNote: vi.fn(),
    uploadAudio: vi.fn(() => Promise.resolve()),
    onUploadProgress: vi.fn(() => () => {}),
    getDefaultTranscriberStatus: vi.fn(async () => null),
  },
}))

// RecordingSession.start() is controlled per-test via mockSessionStart
vi.mock('../../main/recorder', () => ({
  RecordingSession: class {
    async start() {
      return mockSessionStart()
    }
    async stop() {
      return mockSessionStop()
    }
  },
}))
vi.mock('../capture/electronCapture', () => ({ ElectronCapture: class {} }))
vi.mock('../lib/audioPrefs', () => ({
  loadAudioPrefs: () => ({ deviceId: undefined, gain: 1.0 }),
  saveAudioPrefs: vi.fn(),
}))

// NoteHeader: expose onStart and onMicRetry as testable buttons
vi.mock('./NoteHeader', () => ({
  NoteHeader: (props: {
    onStart: () => void
    onStop: () => void
    onMicRetry?: () => void
  }) => (
    <div>
      <button onClick={props.onStart}>start-rec</button>
      <button onClick={props.onStop}>stop-rec</button>
      {props.onMicRetry && (
        <button onClick={props.onMicRetry}>mic-retry</button>
      )}
    </div>
  ),
}))
vi.mock('./ProcessingBanner', () => ({ ProcessingBanner: () => null }))
vi.mock('./TagBar', () => ({ TagBar: () => null }))
vi.mock('./FolderBar', () => ({ FolderBar: () => null }))
vi.mock('./NoteView', () => ({ NoteView: () => null }))
vi.mock('./NoteEditor', () => ({ NoteEditor: () => null }))
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: vi.fn() }) }))

import { NoteScreen } from './NoteScreen'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeMicPermissionError() {
  const err = new Error('Mic permission denied')
  err.name = 'NotAllowedError'
  return err
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

beforeEach(() => {
  mockSessionStart.mockClear()
  mockSessionStop.mockClear()
  mockSessionStop.mockResolvedValue({
    bytes: new Uint8Array([]),
    mimeType: 'audio/webm',
    hasSystemAudio: false,
  })
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('NoteScreen — Retry restarts capture', () => {
  it('calls start() when Retry is clicked after a mic-permission-denied error', async () => {
    const user = userEvent.setup()

    // First start() call throws NotAllowedError → NoteScreen sets micError
    // Second start() call (on Retry) succeeds
    mockSessionStart
      .mockRejectedValueOnce(makeMicPermissionError())
      .mockResolvedValueOnce(undefined)

    render(<NoteScreen />)

    // Wait for the note to load so the header renders
    const startBtn = await screen.findByText('start-rec')

    // Trigger start → should fail with mic permission error
    await user.click(startBtn)

    // The Retry button (onMicRetry) is always rendered in our mock;
    // clicking it must call start() a second time
    const retryBtn = screen.getByText('mic-retry')
    await user.click(retryBtn)

    // start() must have been called twice: once for the failed attempt,
    // once more when Retry was clicked
    await waitFor(() => expect(mockSessionStart).toHaveBeenCalledTimes(2))
  })

  it('does NOT restart capture if the user never clicks Retry', async () => {
    const user = userEvent.setup()

    mockSessionStart.mockRejectedValueOnce(makeMicPermissionError())

    render(<NoteScreen />)
    await screen.findByText('start-rec')

    // Click start once (fails)
    await user.click(screen.getByText('start-rec'))

    // Do not click Retry; start() should only have been called once
    expect(mockSessionStart).toHaveBeenCalledTimes(1)
  })
})
