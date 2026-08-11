// @vitest-environment jsdom
import { beforeEach, describe, it, expect, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route, Outlet, useParams, useSearchParams } from 'react-router-dom'
import { muesli } from '@/api'
import type { Folder, Note, SearchMatch } from '../../shared/types'
import type { ActiveView } from './shell/AppLayout'

const deleteNote = vi.fn()
const addNoteFolder = vi.fn()
const resummarize = vi.fn()
const reorderNoteInFolder = vi.fn()
const notify = vi.fn()
const getManualServer = vi.fn()

vi.mock('@/api', () => ({
  muesli: {
    getManualServer: () => getManualServer(),
    deleteNote: (id: string) => deleteNote(id),
    addNoteFolder: (noteId: string, folderId: string) => addNoteFolder(noteId, folderId),
    resummarize: (id: string) => resummarize(id),
    reorderNoteInFolder: (folderId: string, noteId: string, afterId: string | null) => reorderNoteInFolder(folderId, noteId, afterId),
    pinNote: vi.fn(),
    unpinNote: vi.fn(),
  },
}))

vi.mock('@/components/ui/Toast', () => ({
  useToast: () => ({ notify }),
}))

import { NotesListScreen } from './NotesListScreen'

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

beforeEach(() => {
  getManualServer.mockResolvedValue(false)
})

function OutletStub({ notes, heading = 'All notes', view = { type: 'all' } as ActiveView, loaded = true, folders = [], refresh = () => {}, semanticNotes = [], semanticMatches = {}, semanticSearchAvailable, searchQuery = '' }: {
  notes: Note[]; heading?: string; view?: ActiveView; loaded?: boolean; folders?: Folder[];
  refresh?: () => void; semanticNotes?: Note[]; semanticMatches?: Record<string, SearchMatch[]>; semanticSearchAvailable?: boolean; searchQuery?: string;
}) {
  const isFiltered = view.type !== 'all' || searchQuery.trim() !== ''
  return <Outlet context={{ notes, refresh, heading, isFiltered, view, folders, loaded, semanticNotes, semanticMatches, semanticSearchAvailable, searchQuery, onReorderNote: reorderNoteInFolder }} />
}

function renderWithCtx(notes: Note[], heading?: string, view?: ActiveView, loaded = true) {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route element={<OutletStub notes={notes} heading={heading} view={view} loaded={loaded} />}>
          <Route path="/" element={<NotesListScreen />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

describe('NotesListScreen', () => {
  it('shows loading skeletons when loaded is false', () => {
    renderWithCtx([], 'All notes', undefined, false)
    expect(screen.queryByText(/no notes yet/i)).toBeNull()
    expect(document.querySelector('[data-testid="notes-loading"]')).toBeInTheDocument()
    // Skeleton renders divs with animate-pulse; there should be 6 (1 heading + 5 rows)
    const skeletons = document.querySelectorAll('.animate-pulse')
    expect(skeletons.length).toBeGreaterThanOrEqual(5)
  })

  // USE06 Fix 4: loading state must be announced to screen readers
  it('USE06: loading container has role="status" and aria-label="Loading notes"', () => {
    renderWithCtx([], 'All notes', undefined, false)
    const statusEl = screen.getByRole('status')
    expect(statusEl).toBeInTheDocument()
    expect(statusEl).toHaveAttribute('aria-label', 'Loading notes')
  })
  it('shows the empty-state with no notes', () => {
    renderWithCtx([])
    expect(screen.getByText(/no notes yet/i)).toBeInTheDocument()
  })
  it('shows a filtered empty-state when a view matches nothing', () => {
    renderWithCtx([], 'Standups', { type: 'folder', id: 'f1' })
    expect(screen.getByText(/this folder is empty/i)).toBeInTheDocument()
  })
  it('renders the active-view heading and note rows', () => {
    renderWithCtx([{ id: '1', title: 'Standup', status: 'ready', created_at: '', updated_at: '', partial_transcript: false }], 'Standups', { type: 'folder', id: 'f1' })
    expect(screen.getByRole('heading', { name: 'Standups' })).toBeInTheDocument()
    expect(screen.getByText('Standup')).toBeInTheDocument()
  })
  it('renders day-group headers across buckets', () => {
    const today = new Date(); today.setHours(9, 0, 0, 0)
    const yest = new Date(Date.now() - 86_400_000); yest.setHours(9, 0, 0, 0)
    renderWithCtx(
      [
        { id: '1', title: 'Standup', status: 'ready', created_at: today.toISOString(), updated_at: '', partial_transcript: false },
        { id: '2', title: 'Retro', status: 'ready', created_at: yest.toISOString(), updated_at: '', partial_transcript: false },
      ],
      'All notes',
    )
    expect(screen.getByRole('heading', { name: 'Today' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Yesterday' })).toBeInTheDocument()
    expect(screen.getByText('Standup')).toBeInTheDocument()
    expect(screen.getByText('Retro')).toBeInTheDocument()
  })
  it('renders the page heading in the serif display face', () => {
    renderWithCtx(
      [{ id: '1', title: 'Standup', status: 'ready', created_at: new Date().toISOString(), updated_at: '', partial_transcript: false }],
      'All notes',
    )
    expect(screen.getByRole('heading', { name: 'All notes', level: 1 }).className).toContain('font-serif')
  })
  it('feed rows are draggable and carry the note id', () => {
    const today = new Date()
    renderWithCtx(
      [{ id: 'n9', title: 'Standup', status: 'ready', created_at: today.toISOString(), updated_at: '', partial_transcript: false }],
      'All notes',
    )
    const row = screen.getByText('Standup').closest('[draggable="true"]')!
    expect(row).toHaveAttribute('draggable', 'true')
    const setData = vi.fn()
    fireEvent.dragStart(row, { dataTransfer: { setData } })
    expect(setData).toHaveBeenCalledWith('text/note-id', 'n9')
  })

  it('shows a hover pin control that toggles pin state and refreshes', async () => {
    const pinNote = vi.fn().mockResolvedValue(undefined)
    vi.mocked(muesli.pinNote).mockImplementation(pinNote)
    const { refresh } = renderFeed()
    await userEvent.click(screen.getByRole('button', { name: /pin note/i }))
    await waitFor(() => expect(pinNote).toHaveBeenCalledWith('n1'))
    await waitFor(() => expect(refresh).toHaveBeenCalled())
  })

  it('renders reorder gaps in a folder view and calls onReorderNote when a note drops after a sibling', () => {
    const notes: Note[] = [
      aNote({ id: 'n1', title: 'First' }),
      aNote({ id: 'n2', title: 'Second' }),
    ]
    renderWithCtx(notes, 'Clients', { type: 'folder', id: 'f1' })

    const gap = screen.getByLabelText('reorder gap after First')
    fireEvent.drop(gap, { dataTransfer: { getData: (t: string) => (t === 'text/note-id' ? 'n2' : '') } })

    expect(reorderNoteInFolder).toHaveBeenCalledWith('f1', 'n2', 'n1')
  })

  it('renders a first reorder gap in a folder view and moves the note to the front when it is dropped there', () => {
    const notes: Note[] = [
      aNote({ id: 'n1', title: 'First' }),
      aNote({ id: 'n2', title: 'Second' }),
    ]
    renderWithCtx(notes, 'Clients', { type: 'folder', id: 'f1' })

    const gap = screen.getByLabelText('reorder gap first')
    fireEvent.drop(gap, { dataTransfer: { getData: (t: string) => (t === 'text/note-id' ? 'n2' : '') } })

    expect(reorderNoteInFolder).toHaveBeenCalledWith('f1', 'n2', null)
  })
})

const aNote = (over: Partial<Note> = {}): Note => ({
  id: 'n1', title: 'Weekly standup', status: 'ready', created_at: new Date().toISOString(), updated_at: '', partial_transcript: false, ...over,
} as Note)

const aFolder = (over: Partial<Folder> = {}): Folder => ({
  id: 'f1', name: 'Clients', parent_id: null, created_at: '', ...over,
})

function renderFeed({ notes = [aNote()], folders = [] as Folder[], refresh = vi.fn() } = {}) {
  render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route element={<OutletStub notes={notes} folders={folders} refresh={refresh} />}>
          <Route path="/" element={<NotesListScreen />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
  return { refresh }
}

function openRowMenu() {
  fireEvent.contextMenu(screen.getByText('Weekly standup'))
}

describe('NotesListScreen context menu', () => {
  it('shows the menu items on right-click', async () => {
    renderFeed()
    openRowMenu()
    expect(await screen.findByText('Move to Trash')).toBeInTheDocument()
    expect(screen.getByText(/Add to folder/i)).toBeInTheDocument()
    expect(screen.getByText('Re-run summary')).toBeInTheDocument()
  })

  it('Move to Trash calls deleteNote then refresh', async () => {
    deleteNote.mockResolvedValue(undefined)
    const { refresh } = renderFeed()
    openRowMenu()
    fireEvent.click(await screen.findByText('Move to Trash'))
    await waitFor(() => expect(deleteNote).toHaveBeenCalledWith('n1'))
    await waitFor(() => expect(refresh).toHaveBeenCalled())
  })

  it('Re-run summary calls resummarize then refresh', async () => {
    resummarize.mockResolvedValue(undefined)
    const { refresh } = renderFeed()
    openRowMenu()
    fireEvent.click(await screen.findByText('Re-run summary'))
    await waitFor(() => expect(resummarize).toHaveBeenCalledWith('n1'))
    await waitFor(() => expect(refresh).toHaveBeenCalled())
  })

  it('Re-run summary notifies on error', async () => {
    resummarize.mockRejectedValue(new Error('No transcript to summarize'))
    renderFeed()
    openRowMenu()
    fireEvent.click(await screen.findByText('Re-run summary'))
    await waitFor(() => expect(notify).toHaveBeenCalledWith('No transcript to summarize', 'error'))
  })

  it('Add to folder lists folders and adds the note to the chosen folder', async () => {
    addNoteFolder.mockResolvedValue(undefined)
    const { refresh } = renderFeed({ folders: [aFolder()] })
    openRowMenu()
    const subTrigger = await screen.findByText(/Add to folder/i)
    await userEvent.hover(subTrigger)
    fireEvent.click(subTrigger)
    const item = await screen.findByText('Clients')
    fireEvent.click(item)
    await waitFor(() => expect(addNoteFolder).toHaveBeenCalledWith('n1', 'f1'))
    await waitFor(() => expect(refresh).toHaveBeenCalled())
  })
})


describe('USE01 view-specific empty states', () => {
  it('shows "This folder is empty" when view is folder type and notes is empty', () => {
    render(
      <MemoryRouter initialEntries={['/']}>        <Routes>          <Route element={<OutletStub notes={[]} view={{ type: 'folder', id: 'f1' }} />}>            <Route path="/" element={<NotesListScreen />} />          </Route>        </Routes>      </MemoryRouter>,
    )
    expect(screen.getByText(/this folder is empty/i)).toBeInTheDocument()
    expect(screen.getByText(/drag a note here or start a new meeting/i)).toBeInTheDocument()
  })

  it('shows "No notes match this smart list" when view is list type and notes is empty', () => {
    render(
      <MemoryRouter initialEntries={['/']}>        <Routes>          <Route element={<OutletStub notes={[]} view={{ type: 'list', id: 'l1' }} />}>            <Route path="/" element={<NotesListScreen />} />          </Route>        </Routes>      </MemoryRouter>,
    )
    expect(screen.getByText(/no notes match this smart list/i)).toBeInTheDocument()
  })

  it('shows "No notes with this tag" when view is tag type and notes is empty', () => {
    render(
      <MemoryRouter initialEntries={['/']}>        <Routes>          <Route element={<OutletStub notes={[]} view={{ type: 'tag', tag: 'foo' }} />}>            <Route path="/" element={<NotesListScreen />} />          </Route>        </Routes>      </MemoryRouter>,
    )
    expect(screen.getByText(/no notes with this tag/i)).toBeInTheDocument()
  })

  it('shows "No notes yet" when view is all type and notes is empty', () => {
    render(
      <MemoryRouter initialEntries={['/']}>        <Routes>          <Route element={<OutletStub notes={[]} view={{ type: 'all' }} />}>            <Route path="/" element={<NotesListScreen />} />          </Route>        </Routes>      </MemoryRouter>,
    )
    expect(screen.getByText(/no notes yet/i)).toBeInTheDocument()
  })

  it('shows search empty-state (not folder empty-state) when folder view has a searchQuery', () => {
    render(
      <MemoryRouter initialEntries={['/']}>        <Routes>          <Route element={<OutletStub notes={[]} view={{ type: 'folder', id: 'f1' }} searchQuery="standup" />}>            <Route path="/" element={<NotesListScreen />} />          </Route>        </Routes>      </MemoryRouter>,
    )
    expect(screen.getByText(/no matching notes/i)).toBeInTheDocument()
    expect(screen.queryByText(/this folder is empty/i)).not.toBeInTheDocument()
  })
})

describe('USE09 search result clarity', () => {
  it('shows a "Similar results" heading when semanticNotes is non-empty', () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} semanticNotes={[{ id: 's1', title: 'Semantic hit', status: 'ready', created_at: new Date().toISOString(), updated_at: '', partial_transcript: false }]} view={{ type: 'all' }} searchQuery="standup" />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByText(/similar results/i)).toBeInTheDocument()
  })

  it('does NOT show the no-results empty state when notes is empty but semanticNotes has items', () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} semanticNotes={[{ id: 's1', title: 'Semantic hit', status: 'ready', created_at: new Date().toISOString(), updated_at: '', partial_transcript: false }]} view={{ type: 'all' }} searchQuery="standup" />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.queryByText(/no matching notes/i)).not.toBeInTheDocument()
  })

  it('shows the search query in the empty-state hint when both notes and semanticNotes are empty with a searchQuery', () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} semanticNotes={[]} view={{ type: 'all' }} searchQuery="standup" />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByText(/no matching notes/i)).toBeInTheDocument()
    expect(screen.getByText(/standup/i)).toBeInTheDocument()
  })

  it('distinguishes complete semantic search from lexical-only empty results', () => {
    const { rerender } = render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} semanticNotes={[]} semanticSearchAvailable searchQuery="zephyr" />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByText(/try broader keywords/i)).toBeInTheDocument()
    expect(screen.queryByText(/semantic search is unavailable/i)).not.toBeInTheDocument()

    rerender(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} semanticNotes={[]} semanticSearchAvailable={false} searchQuery="zephyr" />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByText(/semantic search is unavailable/i)).toBeInTheDocument()
  })

  it('shows view-specific message (not embedding an empty query) when view is filtered but searchQuery is empty', () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} semanticNotes={[]} view={{ type: 'list', id: 'l1' }} searchQuery="" />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByText(/no notes match this smart list/i)).toBeInTheDocument()
  })
})

// Route stub that surfaces the resolved note id + `segment` query param so a
// click's navigation target is observable end-to-end (SRC01b point 6/7).
function NoteRouteStub() {
  const { id } = useParams()
  const [params] = useSearchParams()
  return <div data-testid="note-route">{`note=${id ?? ''} segment=${params.get('segment') ?? ''}`}</div>
}

function renderWithMatches(semanticMatches: Record<string, SearchMatch[]>) {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route
          element={
            <OutletStub
              notes={[]}
              semanticNotes={[{ id: 's1', title: 'Semantic hit', status: 'ready', created_at: '', updated_at: '', partial_transcript: false }]}
              semanticMatches={semanticMatches}
              view={{ type: 'all' }}
              searchQuery="standup"
            />
          }
        >
          <Route path="/" element={<NotesListScreen />} />
        </Route>
        <Route path="/notes/:id" element={<NoteRouteStub />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('SRC01b typed search matches — type badges, snippets, and segment jump', () => {
  it('renders a match-type badge and snippet for each match on a semantic hit', () => {
    renderWithMatches({
      s1: [
        { note_id: 's1', match_type: 'transcript', segment_id: 'seg-1', start_ms: 1200, snippet: '…budget review…' },
        { note_id: 's1', match_type: 'summary', snippet: 'summary snippet text' },
      ],
    })
    expect(screen.getByText('Transcript')).toBeInTheDocument()
    expect(screen.getByText('…budget review…')).toBeInTheDocument()
    expect(screen.getByText('Summary')).toBeInTheDocument()
    expect(screen.getByText('summary snippet text')).toBeInTheDocument()
  })

  it('does not render a snippet element for a title match (server omits snippet for title hits)', () => {
    renderWithMatches({ s1: [{ note_id: 's1', match_type: 'title' }] })
    expect(screen.getByText('Title')).toBeInTheDocument()
  })

  it('clicking a transcript match navigates to the note WITH a segment query param', async () => {
    renderWithMatches({
      s1: [{ note_id: 's1', match_type: 'transcript', segment_id: 'seg-42', start_ms: 500, snippet: 'hit text' }],
    })
    await userEvent.click(screen.getByTestId('match-transcript-s1'))
    expect(await screen.findByTestId('note-route')).toHaveTextContent('note=s1 segment=seg-42')
  })

  it('clicking a title match navigates to the note WITHOUT a segment query param', async () => {
    renderWithMatches({ s1: [{ note_id: 's1', match_type: 'title' }] })
    await userEvent.click(screen.getByTestId('match-title-s1'))
    expect(await screen.findByTestId('note-route')).toHaveTextContent('note=s1 segment=')
  })

  it('clicking a summary match navigates to the note WITHOUT a segment query param', async () => {
    renderWithMatches({ s1: [{ note_id: 's1', match_type: 'summary', snippet: 'summary hit' }] })
    await userEvent.click(screen.getByTestId('match-summary-s1'))
    expect(await screen.findByTestId('note-route')).toHaveTextContent('note=s1 segment=')
  })
})

describe('USE04 first-run onboarding hint', () => {
  it('shows the onboarding hint when there are no notes in the all-notes view', () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} view={{ type: 'all' }} />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )
    const hint = document.querySelector('[data-testid="onboarding-hint"]')
    expect(hint).toBeInTheDocument()
    expect(hint!.textContent).toMatch(/new meeting/i)
    expect(hint!.textContent).toMatch(/recording your microphone and system audio/i)
    expect(hint!.textContent).toMatch(/processing happens locally on this device/i)
    expect(hint!.textContent).not.toMatch(/your connected server/i)
    expect(hint!.textContent).toMatch(/processing finishes/i)
    expect(hint!.textContent).toMatch(/appears in this list/i)
    expect(hint!.textContent).not.toMatch(/open the desktop client/i)
    expect(hint!.textContent).not.toMatch(/the desktop client/i)
    expect(hint!.textContent).not.toMatch(/uploads/i)
    expect(hint!.textContent).not.toMatch(/\bthe server\b/i)
  })

  it('uses local-processing copy in embedded mode and keeps the remote wording on a manual server', async () => {
    getManualServer.mockResolvedValueOnce(true)

    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} view={{ type: 'all' }} />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )

    const hint = await screen.findByTestId('onboarding-hint')
    await waitFor(() => expect(hint.textContent).toMatch(/the recording goes to your connected server/i))

    cleanup()
    getManualServer.mockResolvedValue(false)

    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} view={{ type: 'all' }} />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByTestId('onboarding-hint')).toHaveTextContent(/processing happens locally on this device/i)
    expect(screen.getByTestId('onboarding-hint')).not.toHaveTextContent(/goes to your connected server/i)
  })

  it('does NOT show the onboarding hint in a filtered (folder) empty state', () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} view={{ type: 'folder', id: 'f1' }} />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByText(/this folder is empty/i)).toBeInTheDocument()
    expect(document.querySelector('[data-testid="onboarding-hint"]')).not.toBeInTheDocument()
  })

  it('does NOT show the onboarding hint when a search query is active', () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} view={{ type: 'all' }} searchQuery="standup" />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )
    expect(document.querySelector('[data-testid="onboarding-hint"]')).not.toBeInTheDocument()
  })

  it('does NOT show the onboarding hint in a smart-list empty state', () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} view={{ type: 'list', id: 'l1' }} />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByText(/no notes match this smart list/i)).toBeInTheDocument()
    expect(document.querySelector('[data-testid="onboarding-hint"]')).not.toBeInTheDocument()
  })

  it('does NOT show the onboarding hint in a tag empty state', () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<OutletStub notes={[]} view={{ type: 'tag', tag: 'foo' }} />}>
            <Route path="/" element={<NotesListScreen />} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByText(/no notes with this tag/i)).toBeInTheDocument()
    expect(document.querySelector('[data-testid="onboarding-hint"]')).not.toBeInTheDocument()
  })
})
