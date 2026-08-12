// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { FullNote, Note } from '../../shared/types'

const mocks = vi.hoisted(() => ({
  sessionStart: vi.fn(async () => {}),
  startNoteCapture: vi.fn(),
  refresh: vi.fn(),
  notify: vi.fn(),
}))

const draftNote: Note = {
  id: 'note-1',
  title: 'New meeting',
  status: 'draft',
  created_at: '',
  updated_at: '',
  partial_transcript: false,
}

vi.mock('react-router-dom', () => ({
  useParams: () => ({ id: 'note-1' }),
  useSearchParams: () => [new URLSearchParams('capture=1'), () => {}],
  useNavigate: () => vi.fn(),
  useOutletContext: () => ({ allNotes: [], folders: [], refresh: mocks.refresh }),
}))

vi.mock('@/api', () => ({
  muesli: {
    getFull: vi.fn(async (): Promise<FullNote> => ({
      note: draftNote,
      body_markdown: 'Meeting notes',
      transcript: null,
      summaries: [],
    })),
    startNoteCapture: mocks.startNoteCapture,
    addTag: vi.fn(), removeTag: vi.fn(), addNoteFolder: vi.fn(), removeNoteFolder: vi.fn(),
    createFolder: vi.fn(), duplicateNote: vi.fn(), updateBody: vi.fn(), resummarize: vi.fn(),
    regenerateSummary: vi.fn(), listTemplates: vi.fn(async () => []),
    listNoteActionItems: vi.fn(async () => ({ actionItems: [], decisions: [] })),
    listNoteLinks: vi.fn(async () => ({ outgoing: [], backlinks: [] })),
    listRelatedNotes: vi.fn(async () => []), updateActionItem: vi.fn(), deleteNote: vi.fn(),
    createShare: vi.fn(), listNoteShares: vi.fn(async () => []), revokeShare: vi.fn(),
    exportFile: vi.fn(), exportNote: vi.fn(), uploadAudio: vi.fn(),
    onUploadProgress: vi.fn(() => () => {}), checkAudioDedup: vi.fn(async () => ({})),
  },
}))

vi.mock('../../main/recorder', () => ({
  RecordingSession: class {
    start = mocks.sessionStart
  },
}))
vi.mock('../capture/electronCapture', () => ({ ElectronCapture: class {} }))
vi.mock('./NoteHeader', () => ({
  NoteHeader: ({ onStart, recordState }: { onStart: () => void; recordState: string }) => (
    <><button onClick={onStart}>Record</button><div data-testid="status-badge">{recordState === 'recording' ? 'Recording' : 'Draft'}</div></>
  ),
}))
vi.mock('./ProcessingBanner', () => ({ ProcessingBanner: () => null }))
vi.mock('./TagBar', () => ({ TagBar: () => null }))
vi.mock('./FolderBar', () => ({ FolderBar: () => null }))
vi.mock('./NoteView', () => ({ NoteView: () => null }))
vi.mock('./NoteEditor', () => ({ NoteEditor: () => null }))
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: mocks.notify }) }))

import { NoteScreen } from './NoteScreen'

beforeEach(() => {
  vi.clearAllMocks()
  mocks.startNoteCapture.mockResolvedValue({ ...draftNote, status: 'recording' })
})

afterEach(cleanup)

describe('NoteScreen recording start', () => {
  it('keeps a new note Draft until Record is pressed, then transitions it to Recording', async () => {
    render(<NoteScreen />)

    expect(await screen.findByTestId('status-badge')).toHaveTextContent('Draft')
    expect(mocks.startNoteCapture).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: 'Record' }))

    await waitFor(() => expect(mocks.startNoteCapture).toHaveBeenCalledWith('note-1'))
    expect(mocks.sessionStart).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId('status-badge')).toHaveTextContent('Recording')
    expect(mocks.notify).not.toHaveBeenCalled()
  })
})
