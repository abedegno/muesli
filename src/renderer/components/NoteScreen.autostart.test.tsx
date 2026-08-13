// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, cleanup, waitFor } from '@testing-library/react'
import type { FullNote } from '../../shared/types'

const navigate = vi.fn()
const refresh = vi.fn()
const startSpy = vi.fn(async () => {})
const { startNoteCapture } = vi.hoisted(() => ({ startNoteCapture: vi.fn() }))

vi.mock('react-router-dom', () => {
  const searchParams: [URLSearchParams, () => void] = [new URLSearchParams('capture=1&autostart=1'), () => {}]
  return {
    useParams: () => ({ id: 'n1' }),
    useSearchParams: () => searchParams,
    useNavigate: () => navigate,
    useOutletContext: () => ({ allNotes: [], folders: [], refresh }),
  }
})

const fullNote: FullNote = {
  note: { id: 'n1', title: 'Standup', status: 'recording', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: '',
  transcript: null,
  summaries: [],
}

vi.mock('@/api', () => ({
  muesli: {
    getFull: vi.fn(async () => fullNote),
    startNoteCapture,
    addTag: vi.fn(),
    removeTag: vi.fn(),
    addNoteFolder: vi.fn(),
    removeNoteFolder: vi.fn(),
    createFolder: vi.fn(),
    duplicateNote: vi.fn(),
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
    uploadAudio: vi.fn(),
    onUploadProgress: vi.fn(() => () => {}),
    checkAudioDedup: vi.fn(async () => ({})),
  },
}))

vi.mock('../../main/recorder', () => ({
  RecordingSession: class {
    async start() {
      await startSpy()
    }
    async stop() {
      return { bytes: new Uint8Array([]), mimeType: 'audio/webm', hasSystemAudio: false }
    }
  },
}))
vi.mock('../capture/electronCapture', () => ({ ElectronCapture: class {} }))
vi.mock('./NoteHeader', () => ({ NoteHeader: () => <div /> }))
vi.mock('./ProcessingBanner', () => ({ ProcessingBanner: () => null }))
vi.mock('./TagBar', () => ({ TagBar: () => null }))
vi.mock('./FolderBar', () => ({ FolderBar: () => null }))
vi.mock('./NoteView', () => ({ NoteView: () => null }))
vi.mock('./NoteEditor', () => ({ NoteEditor: () => null }))
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: vi.fn() }) }))

import { NoteScreen } from './NoteScreen'

beforeEach(() => {
  navigate.mockClear()
  refresh.mockClear()
  startSpy.mockClear()
  startNoteCapture.mockClear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('NoteScreen autostart query flag', () => {
  it('starts recording once and strips autostart from the URL', async () => {
    render(<NoteScreen />)

    await waitFor(() => expect(startSpy).toHaveBeenCalledTimes(1))
    expect(navigate).toHaveBeenCalledWith('/notes/n1?capture=1', { replace: true })
    expect(startSpy).toHaveBeenCalledTimes(1)
    expect(startNoteCapture).not.toHaveBeenCalled()
  })
})
