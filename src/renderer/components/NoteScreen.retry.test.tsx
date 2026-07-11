// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { FullNote } from '../../shared/types'

// --- Hoisted mocks (run before vi.mock factories) ---------------------------

const { retryNoteMock, getFullMock, refreshMock, failedNote } = vi.hoisted(() => {
  const note: FullNote = {
    note: {
      id: 'note-failed-id',
      title: 'Failed note',
      status: 'failed',
      created_at: '',
      partial_transcript: false,
      updated_at: '',
    },
    body_markdown: '',
    transcript: null,
    summaries: [],
  }
  return {
    retryNoteMock: vi.fn().mockResolvedValue(undefined),
    getFullMock: vi.fn().mockResolvedValue(note),
    refreshMock: vi.fn(),
    failedNote: note,
  }
})

// --- Module mocks -----------------------------------------------------------

const navigate = vi.fn()
vi.mock('react-router-dom', () => {
  const searchParams: [URLSearchParams, () => void] = [new URLSearchParams(''), () => {}]
  return {
    useParams: () => ({ id: 'note-failed-id' }),
    useSearchParams: () => searchParams,
    useNavigate: () => navigate,
    // refreshMock is hoisted so it's safe to reference here.
    useOutletContext: () => ({ allNotes: [], folders: [], refresh: refreshMock }),
  }
})

vi.mock('@/api', () => ({
  muesli: {
    getFull: getFullMock,
    retryNote: retryNoteMock,
    addTag: vi.fn(),
    removeTag: vi.fn(),
    addNoteFolder: vi.fn(),
    removeNoteFolder: vi.fn(),
    createFolder: vi.fn(),
    updateBody: vi.fn(),
    resummarize: vi.fn(),
    regenerateSummary: vi.fn(),
    listTemplates: vi.fn(async () => []),
    deleteNote: vi.fn(),
    exportFile: vi.fn(),
    exportNote: vi.fn(),
    uploadAudio: vi.fn(),
    onUploadProgress: vi.fn(() => () => {}),
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

// ProcessingBanner is NOT mocked so the real Re-run button renders.

import { NoteScreen } from './NoteScreen'

// --- Test lifecycle ---------------------------------------------------------

beforeEach(() => {
  retryNoteMock.mockClear()
  getFullMock.mockClear()
  getFullMock.mockResolvedValue(failedNote)
  refreshMock.mockClear()
  navigate.mockClear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

// --- Tests ------------------------------------------------------------------

describe('NoteScreen retry pipeline', () => {
  it('calls muesli.retryNote and refreshes when Re-run clicked on a failed note', async () => {
    const user = userEvent.setup()

    render(<NoteScreen />)

    // Wait for getFull to resolve and ProcessingBanner's Re-run button to appear.
    const reRunBtn = await screen.findByRole('button', { name: 'Re-run' })

    await user.click(reRunBtn)

    // retryNote must be called with the note id.
    expect(retryNoteMock).toHaveBeenCalledWith('note-failed-id')
    expect(retryNoteMock).toHaveBeenCalledTimes(1)

    // refresh() from the outlet context must be called to trigger re-fetch.
    expect(refreshMock).toHaveBeenCalled()
  })

  it('shows the processing-failed alert for a note in failed status', async () => {
    render(<NoteScreen />)

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Processing failed.')
    })
  })
})
