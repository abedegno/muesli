// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { FullNote } from '../../shared/types'

const state = vi.hoisted(() => ({
  currentNoteId: 'n1' as string,
  refresh: vi.fn(),
  navigate: vi.fn(),
}))

vi.mock('react-router-dom', () => ({
  useParams: () => ({ id: state.currentNoteId }),
  useSearchParams: () => [new URLSearchParams(), vi.fn()],
  useNavigate: () => state.navigate,
  useOutletContext: () => ({ allNotes: [], folders: [], refresh: state.refresh }),
}))

const full: FullNote = {
  note: {
    id: 'n1',
    title: 'Meeting note',
    status: 'ready',
    created_at: '',
    updated_at: '',
    partial_transcript: false,
  },
  body_markdown: '',
  transcript: {
    segments: [
      { start_ms: 0, end_ms: 1000, text: 'alpha transcript here', source: 'mixed' },
      { start_ms: 1000, end_ms: 2000, text: 'something else', source: 'mixed' },
    ],
  },
  summaries: [
    {
      id: 's1',
      template_id: 'tpl-1',
      template_name: 'Default',
      status: 'ready',
      sections: [
        {
          heading: 'Alpha heading',
          content_markdown: 'beta alpha summary',
        },
      ],
    },
  ],
}

vi.mock('@/api', () => ({
  muesli: {
    getFull: vi.fn(async () => full),
    listTemplates: vi.fn(async () => []),
    listNotes: vi.fn(async () => []),
    listNoteLinks: vi.fn(async () => ({ outgoing: [], backlinks: [] })),
    listRelatedNotes: vi.fn(async () => []),
    getNoteAudioUrl: vi.fn(async () => ({})),
    createShare: vi.fn(),
    listNoteShares: vi.fn(async () => []),
    revokeShare: vi.fn(),
    getDefaultTranscriberStatus: vi.fn(),
    duplicateNote: vi.fn(),
    retryNote: vi.fn(),
    processNextNote: vi.fn(),
    resummarize: vi.fn(),
    regenerateSummary: vi.fn(),
    updateBody: vi.fn(),
    updateTitle: vi.fn(),
    addNoteLink: vi.fn(),
    deleteNote: vi.fn(),
    exportNote: vi.fn(),
    onUploadProgress: vi.fn(),
    startNoteStream: vi.fn(),
    stopNoteStream: vi.fn(),
    sendNoteStreamAudio: vi.fn(),
    listSpeakerAliases: vi.fn(async () => []),
    upsertSpeakerAlias: vi.fn(),
  },
}))

vi.mock('./NoteHeader', () => ({
  NoteHeader: ({ title }: { title: string }) => <div data-testid="note-header">{title}</div>,
}))
vi.mock('./ProcessingBanner', () => ({ ProcessingBanner: () => null }))
vi.mock('./TagBar', () => ({ TagBar: () => null }))
vi.mock('./FolderBar', () => ({ FolderBar: () => null }))
vi.mock('./LiveTranscriptPanel', () => ({ LiveTranscriptPanel: () => null }))
vi.mock('./DuplicateAudioDialog', () => ({ DuplicateAudioDialog: () => null }))
vi.mock('./NoteActionItemsPanel', () => ({ NoteActionItemsPanel: () => null }))
vi.mock('./DiarizationReviewPanel', () => ({ DiarizationReviewPanel: () => null }))
vi.mock('./NoteEditor', () => ({ NoteEditor: () => null }))
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: vi.fn() }) }))

import { NoteScreen } from './NoteScreen'

beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn()
  state.refresh.mockClear()
  state.navigate.mockClear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  delete (Element.prototype as unknown as { scrollIntoView?: unknown }).scrollIntoView
})

describe('NoteScreen search', () => {
  it('highlights note matches and steps through them with next/previous controls', async () => {
    const user = userEvent.setup()
    render(<NoteScreen />)

    expect(await screen.findByTestId('note-header')).toHaveTextContent('Meeting note')
    await user.click(screen.getByRole('button', { name: 'Transcript' }))

    const input = screen.getByLabelText('Find in note')
    await user.type(input, 'alpha')

    await waitFor(() => expect(screen.getByText('1/3')).toBeInTheDocument())
    expect(document.querySelectorAll('mark')).toHaveLength(3)

    let current = document.querySelector('[data-note-search-current="true"]')
    expect(current).not.toBeNull()
    expect(current!.parentElement?.tagName).toBe('H3')

    await user.click(screen.getByRole('button', { name: 'Next match' }))
    await waitFor(() => expect(screen.getByText('2/3')).toBeInTheDocument())
    current = document.querySelector('[data-note-search-current="true"]')
    expect(current).not.toBeNull()
    expect(current!.parentElement?.tagName).toBe('P')

    await user.click(screen.getByRole('button', { name: 'Next match' }))
    await waitFor(() => expect(screen.getByText('3/3')).toBeInTheDocument())
    current = document.querySelector('[data-note-search-current="true"]')
    expect(current).not.toBeNull()
    expect(current!.parentElement?.tagName).toBe('SPAN')

    await user.click(screen.getByRole('button', { name: 'Previous match' }))
    await waitFor(() => expect(screen.getByText('2/3')).toBeInTheDocument())
    current = document.querySelector('[data-note-search-current="true"]')
    expect(current).not.toBeNull()
    expect(current!.parentElement?.tagName).toBe('P')

    expect(Element.prototype.scrollIntoView).toHaveBeenCalled()
  })
})
