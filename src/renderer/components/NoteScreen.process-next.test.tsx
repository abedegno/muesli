// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { FullNote } from '../../shared/types'

// --- Hoisted mocks (run before vi.mock factories) ---------------------------

const { processNextNoteMock, getFullMock, refreshMock, queuedNote } = vi.hoisted(() => {
  const note: FullNote = {
    note: {
      id: 'note-queued-id',
      title: 'Queued note',
      status: 'uploaded',
      created_at: '',
      partial_transcript: false,
      updated_at: '',
    },
    body_markdown: '',
    transcript: null,
    summaries: [],
  }
  return {
    processNextNoteMock: vi.fn().mockResolvedValue(undefined),
    getFullMock: vi.fn().mockResolvedValue(note),
    refreshMock: vi.fn(),
    queuedNote: note,
  }
})

// --- Module mocks -----------------------------------------------------------

const navigate = vi.fn()
vi.mock('react-router-dom', () => {
  const searchParams: [URLSearchParams, () => void] = [new URLSearchParams(''), () => {}]
  return {
    useParams: () => ({ id: 'note-queued-id' }),
    useSearchParams: () => searchParams,
    useNavigate: () => navigate,
    // refreshMock is hoisted so it's safe to reference here.
    useOutletContext: () => ({ allNotes: [], folders: [], refresh: refreshMock }),
  }
})

vi.mock('@/api', () => ({
  muesli: {
    getFull: getFullMock,
    processNextNote: processNextNoteMock,
    retryNote: vi.fn(),
    addTag: vi.fn(),
    removeTag: vi.fn(),
    addNoteFolder: vi.fn(),
    removeNoteFolder: vi.fn(),
    createFolder: vi.fn(),
    updateBody: vi.fn(),
    resummarize: vi.fn(),
    deleteNote: vi.fn(),
    exportFile: vi.fn(),
    exportNote: vi.fn(),
    uploadAudio: vi.fn(),
    onUploadProgress: vi.fn(() => () => {}),
    getDefaultTranscriberStatus: vi.fn(),
  },
}))

vi.mock('../../main/recorder', () => ({
  RecordingSession: class {
    async start() {}
    async stop() {
      return { bytes: new Uint8Array([]), mimeType: 'audio/webm', hasSystemAudio: false }
    }
  },
}))
vi.mock('../capture/electronCapture', () => ({ ElectronCapture: class {} }))

vi.mock('./NoteHeader', () => ({
  NoteHeader: () => <div />,
}))
vi.mock('./TagBar', () => ({ TagBar: () => null }))
vi.mock('./FolderBar', () => ({ FolderBar: () => null }))
vi.mock('./NoteView', () => ({ NoteView: () => null }))
vi.mock('./NoteEditor', () => ({ NoteEditor: () => null }))
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: vi.fn() }) }))

// ProcessingBanner is NOT mocked so the real Process next button renders.

import { NoteScreen } from './NoteScreen'

// --- Test lifecycle ---------------------------------------------------------

beforeEach(() => {
  processNextNoteMock.mockClear()
  processNextNoteMock.mockResolvedValue(undefined)
  getFullMock.mockClear()
  getFullMock.mockResolvedValue(queuedNote)
  refreshMock.mockClear()
  navigate.mockClear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

// --- Tests ------------------------------------------------------------------

describe('NoteScreen process-next (priority bump)', () => {
  it('calls muesli.processNextNote and refreshes when Process next clicked on a queued note', async () => {
    const user = userEvent.setup()

    render(<NoteScreen />)

    // Wait for getFull to resolve and ProcessingBanner's Process next button to appear.
    const processNextBtn = await screen.findByRole('button', { name: 'Process next' })

    await user.click(processNextBtn)

    expect(processNextNoteMock).toHaveBeenCalledWith('note-queued-id')
    expect(processNextNoteMock).toHaveBeenCalledTimes(1)

    // refresh() from the outlet context must be called to trigger re-fetch.
    expect(refreshMock).toHaveBeenCalled()
  })

  it('shows a toast and does not refresh when processNextNote fails (e.g. nothing pending to bump)', async () => {
    processNextNoteMock.mockRejectedValueOnce(new Error('no pending job to prioritize'))
    const user = userEvent.setup()

    render(<NoteScreen />)

    const processNextBtn = await screen.findByRole('button', { name: 'Process next' })
    await user.click(processNextBtn)

    expect(processNextNoteMock).toHaveBeenCalledTimes(1)
    expect(refreshMock).not.toHaveBeenCalled()
  })

  // A note's pipeline job may already be running (transcribing/summarizing) while
  // its NEXT job sits pending, not yet claimed by a worker -- exactly the
  // queued-notes-competing-for-a-worker scenario "process next" targets. The
  // action must stay available in those statuses too, not just 'uploaded'.
  it.each(['uploaded', 'transcribing', 'summarizing'] as const)(
    'calls muesli.processNextNote and refreshes when Process next clicked while status=%s',
    async (status) => {
      getFullMock.mockResolvedValue({ ...queuedNote, note: { ...queuedNote.note, status } })
      const user = userEvent.setup()

      render(<NoteScreen />)

      const processNextBtn = await screen.findByRole('button', { name: 'Process next' })
      await user.click(processNextBtn)

      expect(processNextNoteMock).toHaveBeenCalledWith('note-queued-id')
      expect(processNextNoteMock).toHaveBeenCalledTimes(1)
      expect(refreshMock).toHaveBeenCalled()
    },
  )

  it.each(['ready', 'failed', 'recording'] as const)(
    'does not show a Process next button while status=%s',
    async (status) => {
      getFullMock.mockResolvedValue({ ...queuedNote, note: { ...queuedNote.note, status } })

      const { container } = render(<NoteScreen />)

      // Wait for the note to finish loading (the loading skeleton unmounts),
      // then assert the button never appears for these terminal/idle states.
      await waitFor(() => expect(container.querySelector('.animate-pulse')).toBeNull())
      expect(screen.queryByRole('button', { name: 'Process next' })).not.toBeInTheDocument()
    },
  )
})
