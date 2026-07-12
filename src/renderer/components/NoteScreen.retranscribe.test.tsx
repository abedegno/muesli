// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { FullNote } from '../../shared/types'

const navigate = vi.fn()
const refresh = vi.fn()
const notify = vi.fn()

vi.mock('react-router-dom', () => {
  const searchParams: [URLSearchParams, () => void] = [new URLSearchParams(''), () => {}]
  return {
    useParams: () => ({ id: 'n1' }),
    useSearchParams: () => searchParams,
    useNavigate: () => navigate,
    useOutletContext: () => ({ allNotes: [], folders: [], refresh }),
  }
})

const readyNote: FullNote = {
  note: { id: 'n1', title: 'Sprint planning', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: '',
  transcript: null,
  summaries: [],
}

vi.mock('@/lib/pollNote', () => ({
  pollNote: vi.fn(() => new Promise<void>(() => {})),
}))

vi.mock('@/api', () => ({
  muesli: {
    getFull: vi.fn(async () => readyNote),
    retranscribeNote: vi.fn(async () => ({ status: 'transcribing' })),
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
    updateActionItem: vi.fn(),
    deleteNote: vi.fn(),
    exportFile: vi.fn(),
    exportNote: vi.fn(),
    uploadAudio: vi.fn(),
    onUploadProgress: vi.fn(() => () => {}),
    checkAudioDedup: vi.fn(async () => ({})),
    pinNote: vi.fn(),
    unpinNote: vi.fn(),
    linkNoteEvent: vi.fn(),
    unlinkNoteEvent: vi.fn(),
    duplicateNote: vi.fn(),
    getDefaultTranscriberStatus: vi.fn(),
    getNoteAudioUrl: vi.fn(),
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
  NoteHeader: (props: { onRetranscribe?: (options: { model?: string; language?: string }) => Promise<void> }) => (
    <div>
      {props.onRetranscribe && (
        <button onClick={() => { void props.onRetranscribe?.({ model: 'gpt-4o-mini', language: 'en' }) }}>
          enhance-note
        </button>
      )}
    </div>
  ),
}))
vi.mock('./ProcessingBanner', () => ({
  ProcessingBanner: (props: { status: string }) => <div data-testid="processing-status">{props.status}</div>,
}))
vi.mock('./TagBar', () => ({ TagBar: () => null }))
vi.mock('./FolderBar', () => ({ FolderBar: () => null }))
vi.mock('./NoteView', () => ({ NoteView: () => null }))
vi.mock('./NoteEditor', () => ({ NoteEditor: () => null }))
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify }) }))

import { NoteScreen } from './NoteScreen'

beforeEach(() => {
  navigate.mockClear()
  refresh.mockClear()
  notify.mockClear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('NoteScreen retranscribe flow', () => {
  it('calls retranscribeNote and flips the note into transcribing immediately', async () => {
    const user = userEvent.setup()
    render(<NoteScreen />)

    await screen.findByText('enhance-note')
    expect(screen.getByTestId('processing-status')).toHaveTextContent('ready')

    await user.click(screen.getByText('enhance-note'))

    const { muesli } = await import('@/api')
    await waitFor(() => expect(muesli.retranscribeNote).toHaveBeenCalledWith('n1', { model: 'gpt-4o-mini', language: 'en' }))
    await waitFor(() => expect(screen.getByTestId('processing-status')).toHaveTextContent('transcribing'))
    expect(refresh).toHaveBeenCalled()
  })

  it('surfaces a toast when retranscribeNote fails, such as when audio is unavailable', async () => {
    const user = userEvent.setup()
    const { muesli } = await import('@/api')
    vi.mocked(muesli.retranscribeNote).mockRejectedValueOnce(new Error('no stored audio to retranscribe'))

    render(<NoteScreen />)
    await screen.findByText('enhance-note')

    await user.click(screen.getByText('enhance-note'))

    await waitFor(() => expect(notify).toHaveBeenCalledWith('no stored audio to retranscribe', 'error'))
    expect(screen.getByTestId('processing-status')).toHaveTextContent('ready')
  })
})
