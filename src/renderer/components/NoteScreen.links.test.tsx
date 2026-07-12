// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { FullNote, Note, NoteLinksResponse } from '../../shared/types'

const { addNoteLinkMock, getFullMock, listNoteLinksMock, listNotesMock, navigateMock, refreshMock } = vi.hoisted(() => ({
  addNoteLinkMock: vi.fn(),
  getFullMock: vi.fn(),
  listNoteLinksMock: vi.fn(),
  listNotesMock: vi.fn(),
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
    listNotes: (...args: Parameters<typeof listNotesMock>) => listNotesMock(...args),
    addNoteLink: (...args: Parameters<typeof addNoteLinkMock>) => addNoteLinkMock(...args),
    listTemplates: vi.fn(async () => []),
    listNoteActionItems: vi.fn(async () => ({ actionItems: [], decisions: [] })),
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
    exportFile: vi.fn(),
    exportNote: vi.fn(),
    uploadAudio: vi.fn(),
    onUploadProgress: vi.fn(() => () => {}),
    checkAudioDedup: vi.fn(async () => ({})),
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
  getFullMock.mockReset()
  listNoteLinksMock.mockReset()
  listNotesMock.mockReset()
  addNoteLinkMock.mockReset()
  navigateMock.mockReset()
  refreshMock.mockReset()
  getFullMock.mockResolvedValue(note)
  listNoteLinksMock.mockResolvedValue({ outgoing: [], backlinks: [] } satisfies NoteLinksResponse)
  listNotesMock.mockResolvedValue([
    { id: 'n1', title: 'Current note', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
    { id: 'n2', title: 'Project Plan', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
    { id: 'n3', title: 'Weekly Retro', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  ] satisfies Note[])
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('NoteScreen links panel', () => {
  it('filters add-link candidates and adds the selected note', async () => {
    const user = userEvent.setup()
    const addedLink = { id: 'l1', owner_id: 'u1', from_note_id: 'n1', to_note_id: 'n2', created_at: '2026-07-12T00:00:00Z' }
    listNoteLinksMock
      .mockResolvedValueOnce({ outgoing: [], backlinks: [] })
      .mockResolvedValueOnce({ outgoing: [addedLink], backlinks: [] })
    addNoteLinkMock.mockResolvedValue(addedLink)

    render(<NoteScreen />)

    const input = await screen.findByLabelText('Add link')
    await user.type(input, 'proj')

    expect(screen.getByRole('button', { name: 'Project Plan' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Weekly Retro' })).not.toBeInTheDocument()

    await user.keyboard('{ArrowDown}{Enter}')

    expect(addNoteLinkMock).toHaveBeenCalledWith('n1', 'n2')
    expect(await screen.findByRole('heading', { name: 'Outgoing' })).toBeInTheDocument()

    const outgoingHeading = screen.getByRole('heading', { name: 'Outgoing' })
    const outgoingSection = outgoingHeading.parentElement
    expect(outgoingSection).not.toBeNull()
    expect(within(outgoingSection as HTMLElement).getByRole('button', { name: 'Project Plan' })).toBeInTheDocument()
  })

  it('renders outgoing and backlink titles and navigates when a row is clicked', async () => {
    const user = userEvent.setup()
    listNoteLinksMock.mockResolvedValue({
      outgoing: [
        { id: 'l1', owner_id: 'u1', from_note_id: 'n1', to_note_id: 'n2', created_at: '2026-07-12T00:00:00Z' },
        { id: 'l2', owner_id: 'u1', from_note_id: 'n1', to_note_id: 'n4', created_at: '2026-07-12T00:00:01Z' },
      ],
      backlinks: [
        { id: 'l3', owner_id: 'u1', from_note_id: 'n3', to_note_id: 'n1', created_at: '2026-07-12T00:00:02Z' },
      ],
    })

    render(<NoteScreen />)

    const projectPlan = await screen.findByRole('button', { name: 'Project Plan' })
    const fallbackLink = await screen.findByRole('button', { name: 'n4' })
    const weeklyRetro = await screen.findByRole('button', { name: 'Weekly Retro' })

    await user.click(projectPlan)
    expect(navigateMock).toHaveBeenCalledWith('/notes/n2')

    await user.click(fallbackLink)
    expect(navigateMock).toHaveBeenCalledWith('/notes/n4')

    await user.click(weeklyRetro)
    expect(navigateMock).toHaveBeenCalledWith('/notes/n3')
  })

  it('shows a loading state while links are pending', async () => {
    listNoteLinksMock.mockImplementation(() => new Promise<NoteLinksResponse>(() => {}))

    render(<NoteScreen />)

    await screen.findByRole('heading', { name: 'Links' })
    expect(screen.getByText('Loading links...')).toBeInTheDocument()
  })

  it('shows an empty state when both link lists are empty', async () => {
    listNoteLinksMock.mockResolvedValue({ outgoing: [], backlinks: [] })

    render(<NoteScreen />)

    await screen.findByRole('heading', { name: 'Links' })
    expect(screen.getByText('No links yet')).toBeInTheDocument()
  })
})
