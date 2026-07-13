// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Note, RelatedNote } from '../../shared/types'

const { addNoteLinkMock, listRelatedNotesMock, onLinkedMock } = vi.hoisted(() => ({
  addNoteLinkMock: vi.fn(),
  listRelatedNotesMock: vi.fn(),
  onLinkedMock: vi.fn(),
}))

vi.mock('@/api', () => ({
  muesli: {
    listRelatedNotes: (...args: Parameters<typeof listRelatedNotesMock>) => listRelatedNotesMock(...args),
    addNoteLink: (...args: Parameters<typeof addNoteLinkMock>) => addNoteLinkMock(...args),
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

import { RelatedNotesPanel } from './NoteScreen'

const allNotes: Note[] = [
  { id: 'n1', title: 'Current note', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  { id: 'n2', title: 'Project Plan', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  { id: 'n3', title: 'Weekly Retro', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
]

beforeEach(() => {
  addNoteLinkMock.mockReset()
  listRelatedNotesMock.mockReset()
  onLinkedMock.mockReset()
  listRelatedNotesMock.mockResolvedValue([] satisfies RelatedNote[])
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('RelatedNotesPanel', () => {
  it('renders suggestions with titles and links them', async () => {
    const user = userEvent.setup()
    listRelatedNotesMock.mockResolvedValue([
      { note_id: 'n2', score: 0.91 },
      { note_id: 'n3', score: 0.84 },
    ] satisfies RelatedNote[])
    addNoteLinkMock.mockResolvedValue({ id: 'l1', owner_id: 'u1', from_note_id: 'n1', to_note_id: 'n2', created_at: '2026-07-12T00:00:00Z' })

    render(<RelatedNotesPanel noteId="n1" allNotes={allNotes} onOpenNote={vi.fn()} refreshToken={0} onLinked={onLinkedMock} />)

    expect(await screen.findByText('Project Plan')).toBeInTheDocument()
    expect(screen.getByText('Weekly Retro')).toBeInTheDocument()

    await user.click(screen.getAllByRole('button', { name: 'Link' })[0])

    expect(addNoteLinkMock).toHaveBeenCalledWith('n1', 'n2')
    expect(onLinkedMock).toHaveBeenCalled()
  })

  it('shows a loading state while related notes are pending', async () => {
    listRelatedNotesMock.mockImplementation(() => new Promise<RelatedNote[]>(() => {}))

    render(<RelatedNotesPanel noteId="n1" allNotes={allNotes} onOpenNote={vi.fn()} refreshToken={0} />)

    expect(await screen.findByRole('heading', { name: 'Related' })).toBeInTheDocument()
    expect(screen.getByText('Loading related notes...')).toBeInTheDocument()
  })

  it('shows an empty state', async () => {
    listRelatedNotesMock.mockResolvedValue([])

    render(<RelatedNotesPanel noteId="n1" allNotes={allNotes} onOpenNote={vi.fn()} refreshToken={0} />)

    expect(await screen.findByText('No related suggestions')).toBeInTheDocument()
  })

  it('shows load errors', async () => {
    listRelatedNotesMock.mockRejectedValueOnce(new Error('boom'))

    render(<RelatedNotesPanel noteId="n1" allNotes={allNotes} onOpenNote={vi.fn()} refreshToken={0} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not load related notes.')
  })
})
