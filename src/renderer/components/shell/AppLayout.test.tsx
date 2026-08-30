// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { act, render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Link, MemoryRouter, Routes, Route } from 'react-router-dom'
import { AppLayout } from './AppLayout'
import { NotesListScreen } from '../NotesListScreen'

// Mock the preload bridge: AppLayout imports `muesli` from '@/api'.
// We replace the module so listNotes is fully controllable.
const listNotes = vi.fn<() => Promise<import('../../../shared/types').Note[]>>()
const listTags = vi.fn<() => Promise<{ id: string; name: string; count: number }[]>>().mockResolvedValue([])
const renameTag = vi.fn<(id: string, name: string) => Promise<{ id: string; name: string }>>()
  .mockResolvedValue({ id: 't1', name: 'renamed' })
const search = vi.fn<(q: string, opts?: { from?: string; to?: string }) => Promise<import('../../../shared/types').SearchMatch[]>>().mockResolvedValue([])
const getCalendarEvents = vi.fn<() => Promise<import('../../../shared/types').CalendarEvent[]>>().mockResolvedValue([])
const promptShowListeners = new Set<(payload: { event: import('../../../shared/types').CalendarEvent; occurrenceKey: string }) => void>()
const promptClearListeners = new Set<(payload: { occurrenceKey: string }) => void>()
const autoRecordListeners = new Set<(payload: { noteId: string }) => void>()
const meetingDetectionRendererReady = vi.fn().mockResolvedValue(undefined)
const meetingDetectionPromptAccept = vi.fn().mockResolvedValue(undefined)
const meetingDetectionPromptDismiss = vi.fn().mockResolvedValue(undefined)
const startNoteCapture = vi.fn().mockResolvedValue({ id: 'note-created', status: 'recording' })

function emitPromptShow(payload: { event: import('../../../shared/types').CalendarEvent; occurrenceKey: string }) {
  for (const listener of promptShowListeners) listener(payload)
}

function emitPromptClear(payload: { occurrenceKey: string }) {
  for (const listener of promptClearListeners) listener(payload)
}

function emitAutoRecord(payload: { noteId: string }) {
  for (const listener of autoRecordListeners) listener(payload)
}

vi.mock('@/api', () => ({
  muesli: {
    listNotes: () => listNotes(),
    listSmartLists: vi.fn().mockResolvedValue([]),
    listFolders: vi.fn().mockResolvedValue([]),
    listTags: () => listTags(),
    renameTag: (id: string, name: string) => renameTag(id, name),
    search: (q: string, opts?: { from?: string; to?: string }) => search(q, opts),
    getCalendarEvents: () => getCalendarEvents(),
    meetingDetectionRendererReady: () => meetingDetectionRendererReady(),
    meetingDetectionPromptAccept: (occurrenceKey: string) => meetingDetectionPromptAccept(occurrenceKey),
    meetingDetectionPromptDismiss: (occurrenceKey: string) => meetingDetectionPromptDismiss(occurrenceKey),
    startNoteCapture: (noteId: string) => startNoteCapture(noteId),
    onMeetingDetectionPromptShow: (listener: (payload: { event: import('../../../shared/types').CalendarEvent; occurrenceKey: string }) => void) => {
      promptShowListeners.add(listener)
      return () => promptShowListeners.delete(listener)
    },
    onMeetingDetectionPromptClear: (listener: (payload: { occurrenceKey: string }) => void) => {
      promptClearListeners.add(listener)
      return () => promptClearListeners.delete(listener)
    },
    onMeetingDetectionAutoRecord: (listener: (payload: { noteId: string }) => void) => {
      autoRecordListeners.add(listener)
      return () => autoRecordListeners.delete(listener)
    },
  },
}))

// Module-level spy so we can assert on notify calls.
const mockNotify = vi.fn()
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: mockNotify }) }))

const NOTES: import('../../../shared/types').Note[] = [
  { id: '1', title: 'Standup', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  { id: '2', title: 'Budget review', status: 'ready', created_at: '', updated_at: '', snippet: 'quarterly numbers', partial_transcript: false },
]

// The note list lives in the main pane (NotesListScreen, the index child),
// not the sidebar — so mount the real layout + index route.
function renderLayout() {
  return render(
    <MemoryRouter>
      <Routes>
        <Route path="/" element={<AppLayout />}>
          <Route index element={<NotesListScreen />} />
          <Route path="notes/:id" element={<div data-testid="note-route" />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

afterEach(() => {
  cleanup()
  listNotes.mockReset()
  listTags.mockReset()
  listTags.mockResolvedValue([])
  renameTag.mockReset()
  renameTag.mockResolvedValue({ id: 't1', name: 'renamed' })
  search.mockReset()
  search.mockResolvedValue([])
  getCalendarEvents.mockReset()
  getCalendarEvents.mockResolvedValue([])
  meetingDetectionRendererReady.mockReset()
  meetingDetectionRendererReady.mockResolvedValue(undefined)
  meetingDetectionPromptAccept.mockReset()
  meetingDetectionPromptAccept.mockResolvedValue(undefined)
  meetingDetectionPromptDismiss.mockReset()
  meetingDetectionPromptDismiss.mockResolvedValue(undefined)
  startNoteCapture.mockClear()
  promptShowListeners.clear()
  promptClearListeners.clear()
  autoRecordListeners.clear()
  mockNotify.mockClear()
  localStorage.clear()
})

describe('AppLayout route-entry refresh', () => {
  it('refetches notes when a routed child changes without changing the active view', async () => {
    listNotes.mockResolvedValue(NOTES)
    render(
      <MemoryRouter>
        <Routes>
          <Route path="/" element={<AppLayout />}>
            <Route index element={<Link to="/notes/1">Open note</Link>} />
            <Route path="notes/:id" element={<div>Note detail</div>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(listNotes).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('link', { name: 'Open note' }))

    expect(await screen.findByText('Note detail')).toBeInTheDocument()
    await waitFor(() => expect(listNotes).toHaveBeenCalledTimes(2))
  })
})

describe('AppLayout client-side search filter', () => {
  it('shows all notes after initial load', async () => {
    listNotes.mockResolvedValue(NOTES)
    renderLayout()

    expect(await screen.findByText('Standup')).toBeInTheDocument()
    expect(screen.getByText('Budget review')).toBeInTheDocument()
  })

  it('renders pinned notes before unpinned notes in the list', async () => {
    const newer = new Date(2026, 5, 13, 10, 0).toISOString()
    const older = new Date(2026, 5, 12, 10, 0).toISOString()
    listNotes.mockResolvedValue([
      { id: '1', title: 'Standup', status: 'ready', created_at: newer, updated_at: '', partial_transcript: false },
      { id: '2', title: 'Budget review', status: 'ready', created_at: older, updated_at: '', pinned: true, partial_transcript: false },
    ])
    renderLayout()

    await screen.findByText('Standup')
    const pinnedRow = screen.getByText('Budget review').closest('li')!
    const unpinnedRow = screen.getByText('Standup').closest('li')!
    expect(pinnedRow.compareDocumentPosition(unpinnedRow)).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
  })

  it('filters by title (case-insensitive) when query matches title only', async () => {
    listNotes.mockResolvedValue(NOTES)
    renderLayout()

    // Wait for both notes to load first
    await screen.findByText('Standup')

    await userEvent.type(screen.getByRole('textbox', { name: /search notes/i }), 'budget')

    expect(screen.getByText('Budget review')).toBeInTheDocument()
    expect(screen.queryByText('Standup')).not.toBeInTheDocument()
  })

  it('filters by snippet text (proves snippet field is included in search)', async () => {
    listNotes.mockResolvedValue(NOTES)
    renderLayout()

    // Wait for both notes to load first
    await screen.findByText('Standup')

    // 'quarterly' matches the snippet of 'Budget review', not its title
    await userEvent.type(screen.getByRole('textbox', { name: /search notes/i }), 'quarterly')

    expect(screen.getByText('Budget review')).toBeInTheDocument()
    expect(screen.queryByText('Standup')).not.toBeInTheDocument()
  })
})

describe('AppLayout additive semantic search', () => {
  it('appends a semantic-only hit (no lexical match) while keeping lexical matches', async () => {
    listNotes.mockResolvedValue(NOTES)
    // 'finances' matches neither note's title/snippet lexically, but the server
    // returns note '1' (Standup) as a semantic hit for it.
    search.mockResolvedValue([{ note_id: '1', match_type: 'title' }])
    renderLayout()

    await screen.findByText('Standup')

    // Query lexically matches 'Budget review' (snippet 'quarterly numbers'? no — title).
    // Use a query that lexically matches Budget only, and have search return Standup semantically.
    await userEvent.type(screen.getByRole('textbox', { name: /search notes/i }), 'budget')

    // Lexical match stays.
    expect(screen.getByText('Budget review')).toBeInTheDocument()

    // After the 250ms debounce resolves, the semantic-only hit (Standup) is appended.
    await waitFor(() => expect(search).toHaveBeenCalledWith('budget', { from: undefined, to: undefined }))
    await waitFor(() => expect(screen.getByText('Standup')).toBeInTheDocument())
  })

  it('shows only lexical matches when search returns nothing (no regression)', async () => {
    listNotes.mockResolvedValue(NOTES)
    search.mockResolvedValue([])
    renderLayout()

    await screen.findByText('Standup')
    await userEvent.type(screen.getByRole('textbox', { name: /search notes/i }), 'budget')

    await waitFor(() => expect(search).toHaveBeenCalledWith('budget', { from: undefined, to: undefined }))
    expect(screen.getByText('Budget review')).toBeInTheDocument()
    // Standup never lexically matches 'budget' and is not in the semantic ids.
    expect(screen.queryByText('Standup')).not.toBeInTheDocument()
  })
})

describe('AppLayout — stale semantic-search results race (MUST-FIX, prior finding from closed PR #221)', () => {
  it('never applies query A\'s (stale, slow) matches once the query has moved on to a different query B', async () => {
    listNotes.mockResolvedValue(NOTES)

    // Query A ('alpha') never lexically matches either note; its search resolves
    // late, AFTER the user has already moved on to query B.
    let resolveAlpha!: (matches: { note_id: string; match_type: 'title' | 'transcript' | 'summary' }[]) => void
    const alphaPending = new Promise<{ note_id: string; match_type: 'title' | 'transcript' | 'summary' }[]>((resolve) => {
      resolveAlpha = resolve
    })
    search.mockImplementationOnce(() => alphaPending)
    // Query B ('beta') resolves promptly with a DIFFERENT note as its semantic hit.
    search.mockImplementationOnce(() => Promise.resolve([{ note_id: '2', match_type: 'title' }]))

    renderLayout()
    await screen.findByText('Standup')

    const box = screen.getByRole('textbox', { name: /search notes/i })
    await userEvent.type(box, 'alpha')
    await waitFor(() => expect(search).toHaveBeenCalledWith('alpha', { from: undefined, to: undefined }))

    // Neither note lexically matches 'alpha', so with A's search still in
    // flight neither note should currently be visible.
    expect(screen.queryByText('Standup')).not.toBeInTheDocument()
    expect(screen.queryByText('Budget review')).not.toBeInTheDocument()

    // Change the query to B BEFORE A's response resolves.
    await userEvent.clear(box)
    await userEvent.type(box, 'beta')
    await waitFor(() => expect(search).toHaveBeenCalledWith('beta', { from: undefined, to: undefined }))

    // B's own (fresh) semantic hit renders.
    await waitFor(() => expect(screen.getByText('Budget review')).toBeInTheDocument())
    // A's hit (Standup) must not be showing under B's query.
    expect(screen.queryByText('Standup')).not.toBeInTheDocument()

    // Now resolve A's stale response — it must never apply, even after landing.
    resolveAlpha([{ note_id: '1', match_type: 'title' }])
    await new Promise((r) => setTimeout(r, 0))
    expect(screen.queryByText('Standup')).not.toBeInTheDocument()
    // B's own result must still be the only thing live.
    expect(screen.getByText('Budget review')).toBeInTheDocument()
  })
})

describe('AppLayout tag → Save as smart list', () => {
  it('right-clicking a tag and choosing "Save as smart list" opens a seeded create editor', async () => {
    listNotes.mockResolvedValue([
      { id: '1', title: 'Standup', status: 'ready', created_at: '', updated_at: '', tags: ['1on1'], partial_transcript: false },
    ])
    // The sidebar tag list now comes from the server (GET /api/tags).
    listTags.mockResolvedValue([{ id: 't1', name: '1on1', count: 1 }])
    renderLayout()
    await screen.findByText('Standup')

    // Scope to the sidebar nav: the tag chips now rendered on feed rows also
    // contain "1on1", so a global button query would be ambiguous.
    const sidebar = within(screen.getByRole('navigation'))
    fireEvent.contextMenu(await sidebar.findByRole('button', { name: /1on1/i }))
    await userEvent.click(await screen.findByText('Save as smart list'))

    // Seeded create editor (NEW, not edit): title + name prefilled with the tag.
    expect(await screen.findByLabelText('List name')).toHaveValue('1on1')
    expect(screen.getByRole('dialog')).toHaveTextContent('New smart list')
    // It's a create, so there's no Delete button in the editor.
    expect(screen.queryByRole('button', { name: /^Delete$/ })).not.toBeInTheDocument()
  })
})

describe('AppLayout tag rename', () => {
  it('confirming a rename calls muesli.renameTag and refetches the tag list', async () => {
    listNotes.mockResolvedValue([
      { id: '1', title: 'Standup', status: 'ready', created_at: '', updated_at: '', tags: ['1on1'], partial_transcript: false },
    ])
    listTags.mockResolvedValue([{ id: 't1', name: '1on1', count: 1 }])
    renderLayout()
    await screen.findByText('Standup')
    // One initial load of the tag list.
    expect(listTags).toHaveBeenCalledTimes(1)

    // Scope to the sidebar nav: the tag chips now rendered on feed rows also
    // contain "1on1", so a global button query would be ambiguous.
    const sidebar = within(screen.getByRole('navigation'))
    fireEvent.contextMenu(await sidebar.findByRole('button', { name: /1on1/i }))
    await userEvent.click(await screen.findByText('Rename…'))

    const input = await screen.findByLabelText('Tag name')
    expect(input).toHaveValue('1on1')
    await userEvent.clear(input)
    await userEvent.type(input, 'renamed')
    await userEvent.click(screen.getByRole('button', { name: /^Save$/ }))

    await waitFor(() => expect(renameTag).toHaveBeenCalledWith('t1', 'renamed'))
    // refresh() refetches the tag list after a successful rename.
    await waitFor(() => expect(listTags.mock.calls.length).toBeGreaterThan(1))
  })
})

describe('AppLayout ⌘K command palette', () => {
  it('opens the command palette on ⌘K', async () => {
    listNotes.mockResolvedValue(NOTES)
    renderLayout()
    await screen.findByText('Standup')

    expect(screen.queryByLabelText('Command palette')).not.toBeInTheDocument()

    fireEvent.keyDown(window, { key: 'k', metaKey: true })

    expect(await screen.findByLabelText('Command palette')).toBeInTheDocument()
    expect(screen.getByLabelText('Search')).toBeInTheDocument()
  })
})

describe('AppLayout meeting detection loop', () => {
  const buildMeeting = () => {
    const now = Date.now()
    return {
      id: 'cal-1',
      title: 'Weekly sync',
      starts_at: new Date(now - 5 * 60 * 1000).toISOString(),
      ends_at: new Date(now + 25 * 60 * 1000).toISOString(),
      description: '',
      location: '',
      conferencing_url: 'https://meet.example/sync',
      attendees: [],
      source_id: 'source-1',
    }
  }

  beforeEach(() => {
    getCalendarEvents.mockResolvedValue([buildMeeting()])
  })

  it('shows a record prompt, dismisses it, and suppresses the same occurrence on refocus', async () => {
    listNotes.mockResolvedValue([{ id: '1', title: 'Existing', status: 'ready', created_at: '', updated_at: '', partial_transcript: false }])

    renderLayout()
    await waitFor(() => expect(meetingDetectionRendererReady).toHaveBeenCalled())
    const meeting = buildMeeting()
    const occurrenceKey = `${meeting.id}::${meeting.starts_at}`

    await act(async () => {
      emitPromptShow({
        event: meeting,
        occurrenceKey,
      })
    })

    expect(await screen.findByText('Record Weekly sync?')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByRole('status')).not.toBeInTheDocument()

    await act(async () => {
      emitPromptClear({ occurrenceKey })
    })
    expect(screen.queryByText('Record Weekly sync?')).not.toBeInTheDocument()
    expect(meetingDetectionPromptDismiss).toHaveBeenCalledWith(occurrenceKey)
  })

  it('auto-records a fresh meeting when the pref is enabled', async () => {
    listNotes.mockResolvedValue([{ id: '1', title: 'Existing', status: 'ready', created_at: '', updated_at: '', partial_transcript: false }])

    renderLayout()
    await waitFor(() => expect(meetingDetectionRendererReady).toHaveBeenCalled())

    await act(async () => {
      emitAutoRecord({ noteId: 'note-created' })
    })

    expect(await screen.findByTestId('note-route')).toBeInTheDocument()
    expect(startNoteCapture).toHaveBeenCalledWith('note-created')
    expect(meetingDetectionPromptAccept).not.toHaveBeenCalled()
  })
})

describe('AppLayout ? keyboard shortcut', () => {
  it('opens the shortcuts overlay on bare ?', async () => {
    listNotes.mockResolvedValue(NOTES)
    renderLayout()
    await screen.findByText('Standup')

    expect(screen.queryByRole('dialog', { name: /keyboard shortcuts/i })).not.toBeInTheDocument()

    fireEvent.keyDown(window, { key: '?' })

    expect(await screen.findByRole('dialog', { name: /keyboard shortcuts/i })).toBeInTheDocument()
  })

  it('does NOT open the shortcuts overlay when a modifier is held (e.g. ⌘?)', async () => {
    listNotes.mockResolvedValue(NOTES)
    renderLayout()
    await screen.findByText('Standup')

    fireEvent.keyDown(window, { key: '?', metaKey: true })
    fireEvent.keyDown(window, { key: '?', ctrlKey: true })
    fireEvent.keyDown(window, { key: '?', altKey: true })
    fireEvent.keyDown(window, { key: '?', shiftKey: true })

    expect(screen.queryByRole('dialog', { name: /keyboard shortcuts/i })).not.toBeInTheDocument()
  })
})

describe('AppLayout — failed note-list load (Gap 2)', () => {
  it('calls notify with tone="error" when listNotes rejects', async () => {
    listNotes.mockRejectedValueOnce(new Error('Connection refused'))
    renderLayout()

    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'Failed to load notes — check your connection',
        'error',
      ),
    )
  })
})

describe('AppLayout search-loading indicator', () => {
  it('shows search-loading while search is in flight and removes it when done', async () => {
    listNotes.mockResolvedValue(NOTES)

    let resolveSearch!: (matches: import('../../../shared/types').SearchMatch[]) => void
    const pendingSearch = new Promise<import('../../../shared/types').SearchMatch[]>((resolve) => { resolveSearch = resolve })
    search.mockImplementationOnce(() => pendingSearch)

    renderLayout()
    await screen.findByText('Standup')

    await userEvent.type(screen.getByRole('textbox', { name: /search notes/i }), 'standup')

    // Wait for the 250ms debounce to fire and search to be called
    await waitFor(() => expect(search).toHaveBeenCalledWith('standup', { from: undefined, to: undefined }))

    // searching is true now — the indicator should appear above the note list
    await waitFor(() => expect(screen.getByTestId('search-loading')).toBeInTheDocument())

    // Resolve the search — searching becomes false
    resolveSearch([])

    // Indicator should disappear
    await waitFor(() => expect(screen.queryByTestId('search-loading')).not.toBeInTheDocument())
  })

  it('clears search-loading immediately when query changes before previous search resolves', async () => {
    listNotes.mockResolvedValue(NOTES)

    // First search never resolves (stuck in-flight)
    let resolveFirst!: (matches: import('../../../shared/types').SearchMatch[]) => void
    const firstPending = new Promise<import('../../../shared/types').SearchMatch[]>((resolve) => { resolveFirst = resolve })
    search.mockImplementationOnce(() => firstPending)
    // Second search resolves immediately
    search.mockImplementationOnce(() => Promise.resolve([]))

    renderLayout()
    await screen.findByText('Standup')

    const box = screen.getByRole('textbox', { name: /search notes/i })
    // Type first query and wait for its debounce + search call
    await userEvent.type(box, 'standup')
    await waitFor(() => expect(search).toHaveBeenCalledWith('standup', { from: undefined, to: undefined }))
    await waitFor(() => expect(screen.getByTestId('search-loading')).toBeInTheDocument())

    // Now clear and type a new query — the first search is still in-flight
    await userEvent.clear(box)
    await userEvent.type(box, 'budget')

    // After the second debounce fires and resolves, searching must be false
    await waitFor(() => expect(search).toHaveBeenCalledWith('budget', { from: undefined, to: undefined }))
    await waitFor(() => expect(screen.queryByTestId('search-loading')).not.toBeInTheDocument())

    // Clean up the dangling promise
    resolveFirst([])
  })

  it('shows search-loading even when filtered notes list is empty', async () => {
    listNotes.mockResolvedValue(NOTES)

    let resolveSearch!: (matches: import('../../../shared/types').SearchMatch[]) => void
    const pendingSearch = new Promise<import('../../../shared/types').SearchMatch[]>((resolve) => { resolveSearch = resolve })
    search.mockImplementationOnce(() => pendingSearch)

    renderLayout()
    await screen.findByText('Standup')

    // 'zzz' matches nothing lexically → filtered list is empty
    await userEvent.type(screen.getByRole('textbox', { name: /search notes/i }), 'zzz')

    await waitFor(() => expect(search).toHaveBeenCalledWith('zzz', { from: undefined, to: undefined }))

    // Indicator must be visible even though the list is empty (no matching notes)
    await waitFor(() => expect(screen.getByTestId('search-loading')).toBeInTheDocument())

    resolveSearch([])
    await waitFor(() => expect(screen.queryByTestId('search-loading')).not.toBeInTheDocument())
  })
})

describe('AppLayout — responsive container setup (UX06)', () => {
  it('root element carries the app-layout CSS class so container queries can target it', () => {
    listNotes.mockResolvedValue([])
    const { container } = renderLayout()
    const layoutRoot = container.querySelector('.app-layout')
    expect(layoutRoot).toBeInTheDocument()
  })

  it('root element carries the [container-type:inline-size] Tailwind class for CSS container queries', () => {
    listNotes.mockResolvedValue([])
    const { container } = renderLayout()
    const layoutRoot = container.querySelector('.app-layout')
    expect(layoutRoot).not.toBeNull()
    // jsdom does not evaluate stylesheets, so we verify the raw Tailwind
    // arbitrary-property class is present on the element.  The CSS runtime
    // converts it to container-type: inline-size for the real browser.
    expect(layoutRoot!.className).toContain('[container-type:inline-size]')
  })

  it('Sidebar <aside> appears before <main> in the DOM for correct flex-col stacking when narrow', () => {
    listNotes.mockResolvedValue([])
    const { container } = renderLayout()
    const aside = container.querySelector('aside')
    const main = container.querySelector('main')
    expect(aside).toBeInTheDocument()
    expect(main).toBeInTheDocument()
    // Node.DOCUMENT_POSITION_FOLLOWING means main comes after aside in the DOM,
    // which gives the correct visual order when flex-direction: column is applied.
    expect(aside!.compareDocumentPosition(main!)).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
  })
})
