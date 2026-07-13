// @vitest-environment jsdom
/**
 * TEC03 — upload lifecycle coverage for NoteScreen.
 *
 * Covers the 6 gaps identified in the task:
 *   1. start() success path
 *   2. start() failure path
 *   3. stop() happy path (upload → poll → idle → navigate)
 *   4. stop() error path (upload failure → toast + idle)
 *   5. stop() no-session early return
 *   6. mount effect isProcessing() branch (note already processing on mount)
 *
 * Intentionally does NOT duplicate the AbortController micro-race, retry-UI,
 * or note-load-failure tests that already exist in NoteScreen.test.tsx.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { FullNote } from '../../shared/types'

// ---------------------------------------------------------------------------
// Module-level spies — must be declared before vi.mock() factories reference them.
// ---------------------------------------------------------------------------

const navigate = vi.fn()
const mockRefresh = vi.fn()
const mockNotify = vi.fn()

// Mutable start-impl so individual tests can inject a throw without needing to
// reconstruct the whole RecordingSession mock.
let startImpl: () => Promise<void> = async () => {}

// ---------------------------------------------------------------------------
// Route / outlet mock
// ---------------------------------------------------------------------------

vi.mock('react-router-dom', () => {
  // Stable context identity — the mount effect depends on `refresh`, so a new
  // object each render would re-fire the effect unpredictably.
  const ctx = { allNotes: [], folders: [], refresh: () => mockRefresh() }
  const searchParams: [URLSearchParams, () => void] = [new URLSearchParams(''), () => {}]
  return {
    useParams: () => ({ id: 'n1' }),
    useSearchParams: () => searchParams,
    useNavigate: () => navigate,
    useOutletContext: () => ctx,
  }
})

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const readyNote: FullNote = {
  note: { id: 'n1', title: 'Standup', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: '',
  transcript: null,
  summaries: [],
}

const transcribingNote: FullNote = {
  note: { id: 'n1', title: 'Standup', status: 'transcribing', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: '',
  transcript: null,
  summaries: [],
}

// ---------------------------------------------------------------------------
// API mock
// ---------------------------------------------------------------------------

vi.mock('@/api', () => ({
  muesli: {
    getFull: vi.fn(async () => readyNote),
    uploadAudio: vi.fn(async () => {}),
    retryNote: vi.fn(async () => {}),
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
    exportFile: vi.fn(),
    exportNote: vi.fn(),
    onUploadProgress: vi.fn(() => () => {}),
    checkAudioDedup: vi.fn(async () => ({})),
  },
}))

// ---------------------------------------------------------------------------
// Recorder mock — delegates to mutable startImpl so tests can inject errors.
// ---------------------------------------------------------------------------

vi.mock('../../main/recorder', () => ({
  RecordingSession: class {
    async start() { return startImpl() }
    async stop() {
      return { bytes: new Uint8Array([1, 2, 3]), mimeType: 'audio/webm', hasSystemAudio: true }
    }
  },
}))

vi.mock('../capture/electronCapture', () => ({ ElectronCapture: class {} }))

vi.mock('./DuplicateAudioDialog', () => ({
  DuplicateAudioDialog: (props: { existingNoteTitle: string; onOpenExisting: () => void; onTranscribeAgain: () => void }) => (
    <div data-testid="dedup-dialog">
      <span data-testid="dedup-title">{props.existingNoteTitle}</span>
      <button onClick={props.onOpenExisting}>Open existing recording</button>
      <button onClick={props.onTranscribeAgain}>Transcribe again</button>
    </div>
  ),
}))

// ---------------------------------------------------------------------------
// Child component mocks.
// NoteHeader is wired so tests can drive the recording lifecycle AND observe
// the current recordState via a data-testid span.
// ---------------------------------------------------------------------------

vi.mock('./NoteHeader', () => ({
  NoteHeader: (props: { onStart: () => void; onStop: () => void; recordState: string; elapsedMs?: number }) => (
    <div>
      <button onClick={props.onStart}>start-rec</button>
      <button onClick={props.onStop}>stop-rec</button>
      <span data-testid="record-state">{props.recordState}</span>
      <span data-testid="elapsed-ms">{props.elapsedMs ?? 0}</span>
    </div>
  ),
}))
vi.mock('./ProcessingBanner', () => ({ ProcessingBanner: () => null }))
vi.mock('./TagBar', () => ({ TagBar: () => null }))
vi.mock('./FolderBar', () => ({ FolderBar: () => null }))
vi.mock('./NoteView', () => ({ NoteView: () => null }))
vi.mock('./NoteEditor', () => ({ NoteEditor: () => null }))

vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: mockNotify }) }))

// ---------------------------------------------------------------------------
// Import subject after all mocks are registered.
// ---------------------------------------------------------------------------

import { NoteScreen } from './NoteScreen'

// ---------------------------------------------------------------------------
// Per-test reset
// ---------------------------------------------------------------------------

beforeEach(() => {
  startImpl = async () => {}
  navigate.mockClear()
  mockRefresh.mockClear()
  mockNotify.mockClear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Render NoteScreen and wait until the note is fully loaded (header visible). */
async function renderAndLoad() {
  render(<NoteScreen />)
  // findByText waits for getFull to resolve and the header mock to appear.
  await screen.findByText('start-rec')
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('NoteScreen — start() success path', () => {
  it('transitions recordState to "recording" after a successful start()', async () => {
    const user = userEvent.setup()
    await renderAndLoad()

    // Initially idle.
    expect(screen.getByTestId('record-state')).toHaveTextContent('idle')

    await user.click(screen.getByText('start-rec'))

    // RecordingSession.start() resolves (default no-op) -> state must flip.
    await waitFor(() =>
      expect(screen.getByTestId('record-state')).toHaveTextContent('recording'),
    )
  })

  it('resets elapsedMs to 0 after the elapsed-timer has incremented it', async () => {
    // Fake only setInterval/clearInterval and Date so Date.now() + the elapsed
    // interval are controlled; setTimeout remains real so waitFor/findByText work.
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] })
    try {
      const user = userEvent.setup()
      await renderAndLoad()

      // First start — setElapsedMs(0) called, fake interval registered.
      await user.click(screen.getByText('start-rec'))
      await waitFor(() =>
        expect(screen.getByTestId('record-state')).toHaveTextContent('recording'),
      )

      // Advance fake clock by 1000 ms — the 500 ms interval fires twice, setting
      // elapsedMs to ~500 then ~1000. act() flushes the resulting React updates.
      act(() => { vi.advanceTimersByTime(1000) })
      await waitFor(() =>
        expect(Number(screen.getByTestId('elapsed-ms').textContent)).toBeGreaterThan(0),
      )

      // Second start (no explicit stop) calls setElapsedMs(0) — the reset under test.
      await user.click(screen.getByText('start-rec'))
      await waitFor(() =>
        expect(screen.getByTestId('elapsed-ms')).toHaveTextContent('0'),
      )

      // No error at any point.
      expect(mockNotify).not.toHaveBeenCalled()
    } finally {
      vi.useRealTimers()
    }
  })
})

describe('NoteScreen — start() failure path', () => {
  it('shows an error toast and leaves state as "idle" when RecordingSession.start() throws', async () => {
    startImpl = async () => { throw new Error('Microphone access denied') }

    const user = userEvent.setup()
    await renderAndLoad()

    await user.click(screen.getByText('start-rec'))

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith('Microphone access denied', 'error'),
    )
    // State must NOT be left as 'recording' on failure.
    expect(screen.getByTestId('record-state')).toHaveTextContent('idle')
  })

  it('does NOT transition to "recording" when start() throws', async () => {
    startImpl = async () => { throw new Error('Permission denied') }

    const user = userEvent.setup()
    await renderAndLoad()
    await user.click(screen.getByText('start-rec'))

    // Give the async chain time to settle.
    await waitFor(() => expect(mockNotify).toHaveBeenCalledTimes(1))

    // recordState must stay 'idle', not 'recording'.
    expect(screen.getByTestId('record-state')).not.toHaveTextContent('recording')
  })
})

describe('NoteScreen — stop() happy path', () => {
  it('navigates to /notes/:id after a successful upload + poll cycle', async () => {
    const user = userEvent.setup()
    await renderAndLoad()

    await user.click(screen.getByText('start-rec'))
    await waitFor(() =>
      expect(screen.getByTestId('record-state')).toHaveTextContent('recording'),
    )

    await user.click(screen.getByText('stop-rec'))

    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith('/notes/n1', { replace: true }),
    )
  })

  it('calls refresh() and returns recordState to "idle" after a successful stop()', async () => {
    const user = userEvent.setup()
    await renderAndLoad()

    await user.click(screen.getByText('start-rec'))
    await waitFor(() =>
      expect(screen.getByTestId('record-state')).toHaveTextContent('recording'),
    )

    await user.click(screen.getByText('stop-rec'))

    // After the full upload -> poll -> done chain, the component should be idle
    // and refresh() should have been called.
    await waitFor(() =>
      expect(screen.getByTestId('record-state')).toHaveTextContent('idle'),
    )
    // refresh() is called twice in the happy path (before poll + after poll).
    expect(mockRefresh).toHaveBeenCalled()
  })
})

describe('NoteScreen — stop() error path', () => {
  it('shows an error toast and resets to "idle" when uploadAudio rejects', async () => {
    const { muesli } = await import('@/api')
    vi.mocked(muesli.uploadAudio).mockRejectedValueOnce(new Error('Upload failed: quota exceeded'))

    const user = userEvent.setup()
    await renderAndLoad()

    await user.click(screen.getByText('start-rec'))
    await waitFor(() =>
      expect(screen.getByTestId('record-state')).toHaveTextContent('recording'),
    )

    await user.click(screen.getByText('stop-rec'))

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith('Upload failed: quota exceeded', 'error'),
    )
    // State must be reset so the user can try again.
    expect(screen.getByTestId('record-state')).toHaveTextContent('idle')
    // Must NOT navigate on error.
    expect(navigate).not.toHaveBeenCalled()
  })
})

describe('NoteScreen — stop() no-session early return', () => {
  it('is a no-op (does not upload) when stop() is called with no active session', async () => {
    const { muesli } = await import('@/api')

    const user = userEvent.setup()
    await renderAndLoad()

    // Deliberately skip start-rec so sessionRef.current stays null.
    await user.click(screen.getByText('stop-rec'))

    // Give the event loop a moment to settle.
    await new Promise((r) => setTimeout(r, 0))

    // uploadAudio must NOT have been called (getFull IS called, but only once, on mount).
    expect(muesli.uploadAudio).not.toHaveBeenCalled()
    // navigate must NOT have been called.
    expect(navigate).not.toHaveBeenCalled()
    // recordState stays idle.
    expect(screen.getByTestId('record-state')).toHaveTextContent('idle')
  })
})

describe('NoteScreen — mount: isProcessing() branch', () => {
  it('starts polling and returns to "idle" when note status is "transcribing" on mount', async () => {
    const { muesli } = await import('@/api')
    // First call (mount effect): note is in transcribing state -> triggers poll.
    // Second call (inside pollNote): note is ready -> poll terminates immediately.
    vi.mocked(muesli.getFull)
      .mockResolvedValueOnce(transcribingNote)
      .mockResolvedValueOnce(readyNote)

    render(<NoteScreen />)

    // After the mount effect runs + pollNote completes, recordState should be
    // 'idle' and refresh() should have been called.
    await waitFor(() =>
      expect(screen.getByTestId('record-state')).toHaveTextContent('idle'),
    )
    expect(mockRefresh).toHaveBeenCalled()
  })

  it('sets recordState to "processing" while an in-flight poll is running', async () => {
    const { muesli } = await import('@/api')
    // First call resolves to transcribing; second call never resolves so the
    // component stays in the 'processing' state for the duration of the test.
    vi.mocked(muesli.getFull)
      .mockResolvedValueOnce(transcribingNote)
      .mockImplementationOnce(() => new Promise<FullNote>(() => {})) // never resolves

    render(<NoteScreen />)

    // The component must show 'processing' while the poll is in-flight.
    await waitFor(() =>
      expect(screen.getByTestId('record-state')).toHaveTextContent('processing'),
    )
  })
})


describe('NoteScreen — stop() dedup gate', () => {
  it('shows DuplicateAudioDialog and does NOT upload when checkAudioDedup returns a match', async () => {
    const { muesli } = await import('@/api')
    vi.mocked(muesli.checkAudioDedup).mockResolvedValueOnce({
      existingNoteId: 'existing-note-id',
      existingNoteTitle: 'My Old Standup',
    })

    const user = userEvent.setup()
    await renderAndLoad()

    await user.click(screen.getByText('start-rec'))
    await waitFor(() =>
      expect(screen.getByTestId('record-state')).toHaveTextContent('recording'),
    )

    await user.click(screen.getByText('stop-rec'))

    // Dialog must appear
    await waitFor(() => expect(screen.getByTestId('dedup-dialog')).toBeInTheDocument())
    expect(screen.getByTestId('dedup-title')).toHaveTextContent('My Old Standup')

    // Upload must NOT have been triggered
    expect(muesli.uploadAudio).not.toHaveBeenCalled()

    // Record state returns to idle (user can cancel)
    expect(screen.getByTestId('record-state')).toHaveTextContent('idle')
  })

  it('proceeds with upload (no dialog) when checkAudioDedup returns no match', async () => {
    const { muesli } = await import('@/api')
    vi.mocked(muesli.checkAudioDedup).mockResolvedValueOnce({})

    const user = userEvent.setup()
    await renderAndLoad()

    await user.click(screen.getByText('start-rec'))
    await waitFor(() =>
      expect(screen.getByTestId('record-state')).toHaveTextContent('recording'),
    )

    await user.click(screen.getByText('stop-rec'))

    // Upload should be called, no dialog
    await waitFor(() => expect(muesli.uploadAudio).toHaveBeenCalledTimes(1))
    expect(screen.queryByTestId('dedup-dialog')).not.toBeInTheDocument()
  })

  it('proceeds with upload (fail open) when checkAudioDedup throws a network error', async () => {
    const { muesli } = await import('@/api')
    vi.mocked(muesli.checkAudioDedup).mockRejectedValueOnce(new Error('Network error'))

    const user = userEvent.setup()
    await renderAndLoad()

    await user.click(screen.getByText('start-rec'))
    await waitFor(() =>
      expect(screen.getByTestId('record-state')).toHaveTextContent('recording'),
    )

    await user.click(screen.getByText('stop-rec'))

    // Upload should proceed despite the dedup check failure
    await waitFor(() => expect(muesli.uploadAudio).toHaveBeenCalledTimes(1))
    expect(screen.queryByTestId('dedup-dialog')).not.toBeInTheDocument()
  })
})
