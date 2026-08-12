// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { FullNote, NoteStatus } from '../../shared/types'

const state = vi.hoisted(() => ({ status: 'ready' as NoteStatus }))

vi.mock('react-router-dom', () => ({
  useParams: () => ({ id: 'note-1' }),
  useSearchParams: () => [new URLSearchParams(''), () => {}],
  useNavigate: () => vi.fn(),
  useOutletContext: () => ({ allNotes: [], folders: [], refresh: vi.fn() }),
}))

vi.mock('@/api', () => ({
  muesli: {
    getFull: vi.fn(async (): Promise<FullNote> => ({
      note: {
        id: 'note-1',
        title: 'Finished meeting',
        status: state.status,
        created_at: '',
        updated_at: '',
        partial_transcript: false,
      },
      body_markdown: '',
      transcript: {
        segments: [
          {
            id: 'segment-1',
            start_ms: 0,
            end_ms: 1000,
            text: 'Playback remains visible.',
            source: 'microphone',
          },
        ],
      },
      summaries: [],
    })),
    addTag: vi.fn(), removeTag: vi.fn(), addNoteFolder: vi.fn(), removeNoteFolder: vi.fn(),
    createFolder: vi.fn(), updateBody: vi.fn(), resummarize: vi.fn(), regenerateSummary: vi.fn(),
    listTemplates: vi.fn(async () => []),
    listNoteActionItems: vi.fn(async () => ({ actionItems: [], decisions: [] })),
    listNoteLinks: vi.fn(async () => ({ outgoing: [], backlinks: [] })),
    listRelatedNotes: vi.fn(async () => []), updateActionItem: vi.fn(), deleteNote: vi.fn(),
    createShare: vi.fn(), listNoteShares: vi.fn(async () => []), revokeShare: vi.fn(),
    exportFile: vi.fn(), exportNote: vi.fn(), uploadAudio: vi.fn(),
    onUploadProgress: vi.fn(() => () => {}), checkAudioDedup: vi.fn(async () => ({})),
  },
}))

vi.mock('../../main/recorder', () => ({ RecordingSession: class {} }))
vi.mock('../capture/electronCapture', () => ({ ElectronCapture: class {} }))
vi.mock('./NoteHeader', () => ({
  NoteHeader: ({ disabledReason }: { disabledReason?: string }) => (
    <div>{disabledReason ? <div role="status">{disabledReason}</div> : <button>Record</button>}</div>
  ),
}))
vi.mock('./ProcessingBanner', () => ({ ProcessingBanner: () => null }))
vi.mock('./TagBar', () => ({ TagBar: () => null }))
vi.mock('./FolderBar', () => ({ FolderBar: () => null }))
vi.mock('./NoteView', () => ({
  NoteView: ({ full }: { full: FullNote }) => (
    <div aria-label="playback">{full.transcript?.segments[0]?.text}</div>
  ),
}))
vi.mock('./NoteEditor', () => ({ NoteEditor: () => null }))
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: vi.fn() }) }))

import { NoteScreen } from './NoteScreen'

beforeEach(() => { state.status = 'ready' })
afterEach(cleanup)

describe('NoteScreen record availability', () => {
  it('disables a new capture on a ready note without obscuring playback', async () => {
    render(<NoteScreen />)

    expect(await screen.findByRole('status')).toHaveTextContent('This note already has a recording')
    expect(screen.getByRole('status')).not.toHaveTextContent(/unavailable/i)
    expect(screen.queryByRole('button', { name: 'Record' })).not.toBeInTheDocument()
    expect(screen.getByLabelText('playback')).toHaveTextContent('Playback remains visible.')
  })

  it('keeps the Record action available for a recording-status note', async () => {
    state.status = 'recording'
    render(<NoteScreen />)

    expect(await screen.findByRole('button', { name: 'Record' })).toBeEnabled()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })
})
