// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { FullNote, Note, NoteLinksResponse } from '../../shared/types'

const {
  getFullMock,
  listNoteLinksMock,
  listNoteSharesMock,
  listNotesMock,
  listRelatedNotesMock,
  navigateMock,
  refreshMock,
} = vi.hoisted(() => ({
  getFullMock: vi.fn(),
  listNoteLinksMock: vi.fn(),
  listNoteSharesMock: vi.fn(),
  listNotesMock: vi.fn(),
  listRelatedNotesMock: vi.fn(),
  navigateMock: vi.fn(),
  refreshMock: vi.fn(),
}))

vi.mock('react-router-dom', () => {
  const searchParams: [URLSearchParams, () => void] = [new URLSearchParams(''), () => {}]
  const ctx = {
    allNotes: [
      { id: 'n1', title: 'Current note', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
      { id: 'n2', title: 'Project Plan', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
      { id: 'n3', title: 'Weekly Retro', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
    ] satisfies Note[],
    folders: [],
    refresh: refreshMock,
  }
  return {
    useParams: () => ({ id: 'n1' }),
    useSearchParams: () => searchParams,
    useNavigate: () => navigateMock,
    useOutletContext: () => ctx,
  }
})

const note: FullNote = {
  note: { id: 'n1', title: 'Current note', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: '',
  transcript: null,
  summaries: [],
}

vi.mock('@/api', () => ({
  muesli: {
    getFull: (...args: Parameters<typeof getFullMock>) => getFullMock(...args),
    listNoteLinks: (...args: Parameters<typeof listNoteLinksMock>) => listNoteLinksMock(...args),
    listNoteShares: (...args: Parameters<typeof listNoteSharesMock>) => listNoteSharesMock(...args),
    listNotes: (...args: Parameters<typeof listNotesMock>) => listNotesMock(...args),
    listRelatedNotes: (...args: Parameters<typeof listRelatedNotesMock>) => listRelatedNotesMock(...args),
    listTemplates: vi.fn(async () => []),
    listNoteActionItems: vi.fn(async () => ({ actionItems: [], decisions: [] })),
    addNoteLink: vi.fn(),
    addTag: vi.fn(),
    removeTag: vi.fn(),
    addNoteFolder: vi.fn(),
    removeNoteFolder: vi.fn(),
    createFolder: vi.fn(),
    updateBody: vi.fn(),
    resummarize: vi.fn(),
    regenerateSummary: vi.fn(),
    updateActionItem: vi.fn(),
    deleteNote: vi.fn(),
    createShare: vi.fn(),
    revokeShare: vi.fn(),
    exportFile: vi.fn(),
    exportNote: vi.fn(),
    uploadAudio: vi.fn(),
    onUploadProgress: vi.fn(() => () => {}),
    checkAudioDedup: vi.fn(async () => ({})),
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
  getFullMock.mockReset().mockResolvedValue(note)
  listNoteLinksMock.mockReset().mockResolvedValue({ outgoing: [], backlinks: [] } satisfies NoteLinksResponse)
  listNoteSharesMock.mockReset().mockResolvedValue([])
  listNotesMock.mockReset().mockResolvedValue([
    { id: 'n1', title: 'Current note', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
    { id: 'n2', title: 'Project Plan', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
    { id: 'n3', title: 'Weekly Retro', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  ] satisfies Note[])
  listRelatedNotesMock.mockReset().mockResolvedValue([])
  navigateMock.mockReset()
  refreshMock.mockReset()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('NoteScreen details disclosure', () => {
  it('Details starts collapsed', async () => {
    render(<NoteScreen />)

    const details = await screen.findByRole('button', { name: 'Details' })
    expect(details).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('heading', { name: 'Sharing' })).toBeNull()
  })

  it('opening Details reveals all four panels', async () => {
    const user = userEvent.setup()

    render(<NoteScreen />)

    const details = await screen.findByRole('button', { name: 'Details' })
    await user.click(details)

    expect(details).toHaveAttribute('aria-expanded', 'true')
    expect(await screen.findByRole('heading', { name: 'Sharing' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Links' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Related' })).toBeInTheDocument()
  })

  it('Details is keyboard-operable', async () => {
    const user = userEvent.setup()

    render(<NoteScreen />)

    const details = await screen.findByRole('button', { name: 'Details' })
    await user.tab()
    expect(details).toHaveFocus()

    await user.keyboard('{Enter}')

    expect(details).toHaveAttribute('aria-expanded', 'true')
    expect(await screen.findByRole('heading', { name: 'Sharing' })).toBeInTheDocument()
  })
})
