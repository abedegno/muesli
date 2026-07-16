// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { FullNote, Share } from '../../shared/types'

const {
  clipboardWriteTextMock,
  createShareMock,
  getFullMock,
  listNoteSharesMock,
  refreshMock,
  revokeShareMock,
} = vi.hoisted(() => ({
  clipboardWriteTextMock: vi.fn(),
  createShareMock: vi.fn(),
  getFullMock: vi.fn(),
  listNoteSharesMock: vi.fn(),
  refreshMock: vi.fn(),
  revokeShareMock: vi.fn(),
}))

vi.mock('react-router-dom', () => {
  const searchParams: [URLSearchParams, () => void] = [new URLSearchParams(''), () => {}]
  return {
    useParams: () => ({ id: 'n1' }),
    useSearchParams: () => searchParams,
    useNavigate: () => vi.fn(),
    useOutletContext: () => ({ allNotes: [], folders: [], refresh: refreshMock }),
  }
})

const fullNote: FullNote = {
  note: { id: 'n1', title: 'Sprint planning', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: '',
  transcript: null,
  summaries: [],
}

const initialShare: Share = {
  id: 'share-1',
  token: 'token-1',
  note_id: 'n1',
  owner_id: 'owner-1',
  created_at: '2026-07-13T00:00:00Z',
}

const createdShare: Share = {
  id: 'share-2',
  token: 'token-2',
  note_id: 'n1',
  owner_id: 'owner-1',
  created_at: '2026-07-13T00:01:00Z',
}

vi.mock('@/api', () => ({
  muesli: {
    getFull: getFullMock,
    listNoteShares: listNoteSharesMock,
    createShare: createShareMock,
    revokeShare: revokeShareMock,
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
    uploadAudio: vi.fn(),
    onUploadProgress: vi.fn(() => () => {}),
    checkAudioDedup: vi.fn(async () => ({})),
    getDefaultTranscriberStatus: vi.fn(),
  },
}))

vi.mock('@/lib/clipboard', () => ({
  writeClipboardText: (text: string) => clipboardWriteTextMock(text),
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
vi.mock('./NoteHeader', () => ({ NoteHeader: () => <div /> }))
vi.mock('./ProcessingBanner', () => ({ ProcessingBanner: () => null }))
vi.mock('./TagBar', () => ({ TagBar: () => null }))
vi.mock('./FolderBar', () => ({ FolderBar: () => null }))
vi.mock('./NoteActionItemsPanel', () => ({ NoteActionItemsPanel: () => null }))
vi.mock('./LiveTranscriptPanel', () => ({ LiveTranscriptPanel: () => null }))
vi.mock('./NoteView', () => ({ NoteView: () => null }))
vi.mock('./NoteEditor', () => ({ NoteEditor: () => null }))
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: vi.fn() }) }))

import { NoteScreen } from './NoteScreen'

beforeEach(() => {
  getFullMock.mockReset().mockResolvedValue(fullNote)
  listNoteSharesMock.mockReset()
  createShareMock.mockReset()
  revokeShareMock.mockReset()
  refreshMock.mockReset()
  clipboardWriteTextMock.mockReset()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('NoteScreen sharing', () => {
  it('creates a share, copies the link, and shows confirmation in the UI', async () => {
    const user = userEvent.setup()
    listNoteSharesMock
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([createdShare])
    createShareMock.mockResolvedValue({ token: createdShare.token, url: 'http://example.test/shared/token-2' })

    render(<NoteScreen />)

    await user.click(await screen.findByRole('button', { name: 'Details' }))
    await screen.findByRole('heading', { name: 'Sharing' })
    await user.click(screen.getByRole('button', { name: 'Create share link' }))

    await waitFor(() => expect(createShareMock).toHaveBeenCalledWith('n1', undefined))
    await waitFor(() => expect(clipboardWriteTextMock).toHaveBeenCalledWith('http://example.test/shared/token-2'))
    expect(await screen.findByText('Share link copied to clipboard: http://example.test/shared/token-2')).toBeInTheDocument()
    expect(await screen.findByText('Public share')).toBeInTheDocument()
  })

  it('revokes a share and removes it from the visible list', async () => {
    const user = userEvent.setup()
    listNoteSharesMock.mockResolvedValueOnce([initialShare])
    revokeShareMock.mockResolvedValue(undefined)

    render(<NoteScreen />)

    await user.click(await screen.findByRole('button', { name: 'Details' }))
    await screen.findByText('Public share')
    await user.click(screen.getByRole('button', { name: 'Revoke' }))

    await waitFor(() => expect(revokeShareMock).toHaveBeenCalledWith('token-1'))
    await waitFor(() => expect(screen.queryByText('Public share')).not.toBeInTheDocument())
  })
})
