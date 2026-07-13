// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { FullNote } from '../../shared/types'

// --- Mocks -----------------------------------------------------------------
// NoteScreen pulls in a heavy tree (TipTap editor, router context, recorder,
// toast). We mock everything except the unit under test — NoteScreen's own
// stop()/unmount AbortController lifecycle — so the test stays focused and
// deterministic. pollNote is intentionally NOT mocked: the real implementation
// is what observes the aborted signal on the navigate-away path.

const testState = vi.hoisted(() => ({
  navigate: vi.fn(),
  refresh: vi.fn(),
  currentNoteId: 'n1' as string,
  resolveTagMutation: {
    addTag: null as null | (() => void),
    removeTag: null as null | (() => void),
  },
}))
vi.mock('react-router-dom', () => {
  // Stable context/searchParams identities: the mount effect depends on
  // `refresh`, so a fresh object per render would re-run it (and spawn extra
  // AbortControllers), defeating the controller-counting assertions.
  const ctx = { allNotes: [], folders: [], refresh: testState.refresh }
  // Includes `segment` so the SRC01b jump-to-timestamp wiring (NoteScreen reads
  // it and threads it to NoteView as `initialSegmentId`) is exercised below.
  const searchParams: [URLSearchParams, () => void] = [new URLSearchParams('segment=seg-99'), () => {}]
  return {
    useParams: () => ({ id: testState.currentNoteId }),
    useSearchParams: () => searchParams,
    useNavigate: () => testState.navigate,
    useOutletContext: () => ctx,
  }
})

// A deferred upload promise the test controls — it stays UNRESOLVED so the
// upload is in-flight while we navigate away.
let resolveUpload: () => void = () => {}
let uploadPromise: Promise<void>
function freshUploadDeferred() {
  uploadPromise = new Promise<void>((res) => {
    resolveUpload = res
  })
}

const fullNote: FullNote = {
  note: { id: 'n1', title: 'Standup', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  body_markdown: '',
  transcript: null,
  summaries: [],
}
const fullNoteById: Record<string, FullNote> = {
  n1: fullNote,
  n2: {
    note: { id: 'n2', title: 'Planning', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
    body_markdown: '',
    transcript: null,
    summaries: [],
  },
}

vi.mock('@/api', () => ({
  muesli: {
    // Terminal status on load → the mount effect does not start a poll.
    getFull: vi.fn(async (id: string) => fullNoteById[id] ?? fullNote),
    uploadAudio: vi.fn(() => uploadPromise),
    addTag: vi.fn(() => new Promise<void>((res) => { testState.resolveTagMutation.addTag = res })),
    removeTag: vi.fn(() => new Promise<void>((res) => { testState.resolveTagMutation.removeTag = res })),
    addNoteFolder: vi.fn(),
    removeNoteFolder: vi.fn(),
    createFolder: vi.fn(),
    duplicateNote: vi.fn(async () => fullNoteById.n2.note),
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
    onUploadProgress: vi.fn(() => () => {}),
    checkAudioDedup: vi.fn(async () => ({})),
  },
}))

// Recorder + capture: start()/stop() resolve so stop() can reach the upload.
vi.mock('../../main/recorder', () => ({
  RecordingSession: class {
    async start() {}
    async stop() {
      return { bytes: new Uint8Array([1, 2, 3]), mimeType: 'audio/webm', hasSystemAudio: true }
    }
  },
}))
vi.mock('../capture/electronCapture', () => ({ ElectronCapture: class {} }))

// Child components reduced to inert stand-ins. NoteHeader exposes the start/stop
// and duplicate callbacks as buttons so the test can drive the lifecycle.
vi.mock('./NoteHeader', () => ({
  NoteHeader: (props: { title: string; onStart: () => void; onStop: () => void; onDeleteNote: () => void; onDuplicate: () => void }) => (
    <div>
      <span data-testid="note-title">{props.title}</span>
      <button onClick={props.onStart}>start-rec</button>
      <button onClick={props.onStop}>stop-rec</button>
      <button onClick={props.onDeleteNote}>delete-note</button>
      <button onClick={props.onDuplicate}>duplicate-note</button>
    </div>
  ),
}))
vi.mock('./ProcessingBanner', () => ({ ProcessingBanner: () => null }))
vi.mock('./TagBar', () => ({
  TagBar: (props: { onAdd?: (name: string) => void; onRemove?: (name: string) => void }) => (
    <div>
      <button onClick={() => props.onAdd?.('urgent')}>add-tag</button>
      <button onClick={() => props.onRemove?.('urgent')}>remove-tag</button>
    </div>
  ),
}))
vi.mock('./FolderBar', () => ({ FolderBar: () => null }))
vi.mock('./NoteView', () => ({
  NoteView: (props: { onRegenerateTemplate?: (templateId: string) => void; regeneratingTemplateId?: string | null; initialSegmentId?: string }) => (
    <div>
      {props.onRegenerateTemplate && (
        <button
          onClick={() => props.onRegenerateTemplate?.('tpl-1')}
          disabled={props.regeneratingTemplateId === 'tpl-1'}
        >
          regenerate-tpl-1
        </button>
      )}
      <span data-testid="regenerating-template-id">{props.regeneratingTemplateId ?? ''}</span>
      <span data-testid="initial-segment-id">{props.initialSegmentId ?? ''}</span>
    </div>
  ),
}))
vi.mock('./NoteEditor', () => ({ NoteEditor: () => null }))

// Module-level spy so tests can assert on notify calls.
const mockNotify = vi.fn()
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: mockNotify }) }))

import { NoteScreen } from './NoteScreen'

// --- AbortController instrumentation ---------------------------------------
// Track every AbortController constructed during a test and keep abort() as a
// spy that still performs the real abort (so signal.aborted flips to true).
const RealAbortController = globalThis.AbortController
let created: AbortController[] = []

beforeEach(() => {
  created = []
  freshUploadDeferred()
  testState.navigate.mockClear()
  testState.refresh.mockClear()
  mockNotify.mockClear()
  testState.currentNoteId = 'n1'
  testState.resolveTagMutation.addTag = null
  testState.resolveTagMutation.removeTag = null
  class TrackedAbortController extends RealAbortController {
    constructor() {
      super()
      vi.spyOn(this as AbortController, 'abort')
      created.push(this)
    }
  }
  globalThis.AbortController = TrackedAbortController as unknown as typeof AbortController
})

afterEach(() => {
  globalThis.AbortController = RealAbortController
  cleanup()
  vi.restoreAllMocks()
})

// Drive the component to the in-flight-upload point: render → record → stop,
// and return the AbortController(s) that stop() created (excluding the mount
// effect's controller). When `created` count grows past `before`, stop()'s
// `new AbortController()` (which precedes the upload await) has run.
async function recordThenStopMidUpload(user: ReturnType<typeof userEvent.setup>) {
  render(<NoteScreen />)
  // Wait for getFull to resolve so the header (and its buttons) render.
  await user.click(await screen.findByText('start-rec'))
  const before = created.length
  await user.click(screen.getByText('stop-rec'))
  // uploadAudio being called proves stop() advanced past `new AbortController()`
  // (line precedes the upload await) and is now parked on the in-flight upload.
  const { muesli } = await import('@/api')
  await waitFor(() => expect(muesli.uploadAudio).toHaveBeenCalledTimes(1))
  return created.slice(before)
}

describe('NoteScreen — upload AbortController micro-race', () => {
  it('aborts the controller created in stop() when the user navigates away mid-upload', async () => {
    const user = userEvent.setup()
    const stopControllers = await recordThenStopMidUpload(user)

    // Because the controller is created BEFORE the upload await, exactly one
    // exists and is abortable the moment the upload goes in-flight.
    expect(stopControllers).toHaveLength(1)
    const controller = stopControllers[0]
    expect(controller.signal.aborted).toBe(false)

    // Navigate away while the upload is still in-flight → unmount cleanup runs
    // pollAbortRef.current?.abort() against a non-undefined controller.
    cleanup()

    expect(controller.abort).toHaveBeenCalled()
    expect(controller.signal.aborted).toBe(true)
  })

  it('does not throw or leave an unhandled rejection when the upload settles after navigate-away', async () => {
    const rejections: unknown[] = []
    const onRejection = (e: PromiseRejectionEvent) => {
      e.preventDefault?.()
      rejections.push(e.reason)
    }
    window.addEventListener('unhandledrejection', onRejection)
    try {
      const user = userEvent.setup()
      const stopControllers = await recordThenStopMidUpload(user)
      expect(stopControllers).toHaveLength(1)

      // User leaves mid-upload, THEN the upload finally resolves. stop() resumes
      // into pollNote with an already-aborted signal, which throws 'aborted' and
      // is swallowed by stop()'s catch — no state update, no escaping rejection.
      cleanup()
      expect(stopControllers[0].signal.aborted).toBe(true)
      resolveUpload()
      // Flush the upload continuation + pollNote's synchronous abort throw.
      await new Promise((r) => setTimeout(r, 0))
      await Promise.resolve()

      expect(rejections).toEqual([])
      // The aborted path must not navigate (that only happens on a clean finish).
      expect(testState.navigate).not.toHaveBeenCalled()
    } finally {
      window.removeEventListener('unhandledrejection', onRejection)
    }
  })

  it('would regress: if no controller is created before the upload await, the navigate-away path has nothing to abort', async () => {
    // This guards the ordering invariant. With the correct source, stop()
    // creates its controller before awaiting the upload, so a controller is
    // present at unmount. If someone moved `new AbortController()` to AFTER the
    // upload await, `created.slice(before)` here would be empty and this
    // expectation — the regression sentinel — would fail.
    const user = userEvent.setup()
    const stopControllers = await recordThenStopMidUpload(user)
    expect(stopControllers.length).toBeGreaterThan(0)
  })
})

describe('NoteScreen — duplicate note action', () => {
  it('duplicates the current note and navigates to the new note', async () => {
    const user = userEvent.setup()
    render(<NoteScreen />)
    await screen.findByText('Standup')

    await user.click(screen.getByText('duplicate-note'))

    const { muesli } = await import('@/api')
    await waitFor(() => expect(muesli.duplicateNote).toHaveBeenCalledWith('n1'))
    expect(testState.navigate).toHaveBeenCalledWith('/notes/n2')
  })
})

describe('NoteScreen — tag refresh abort on route change', () => {
  it.each([
    ['add-tag', 'addTag'],
    ['remove-tag', 'removeTag'],
  ] as const)('does not let a stale %s refresh overwrite the next note', async (buttonLabel, apiMethod) => {
    const user = userEvent.setup()
    const { muesli } = await import('@/api')

    testState.currentNoteId = 'n1'
    const { rerender } = render(<NoteScreen />)

    await screen.findByText('start-rec')
    await waitFor(() => expect(screen.getByTestId('note-title')).toHaveTextContent('Standup'))

    await user.click(screen.getByText(buttonLabel))
    expect(vi.mocked(muesli[apiMethod])).toHaveBeenCalledWith('n1', 'urgent')

    testState.currentNoteId = 'n2'
    rerender(<NoteScreen />)

    await waitFor(() => expect(screen.getByTestId('note-title')).toHaveTextContent('Planning'))

    const callsBefore = vi.mocked(muesli.getFull).mock.calls.length
    testState.resolveTagMutation[apiMethod]?.()
    await waitFor(() => expect(vi.mocked(muesli.getFull).mock.calls.length).toBe(callsBefore))

    expect(screen.getByTestId('note-title')).toHaveTextContent('Planning')
  })
})

describe('NoteScreen — failed note load (Gap 1)', () => {
  it('calls notify with tone="error" when getFull rejects', async () => {
    const { muesli } = await import('@/api')
    vi.mocked(muesli.getFull).mockRejectedValueOnce(new Error('Network error'))

    render(<NoteScreen />)

    await waitFor(() => expect(mockNotify).toHaveBeenCalledWith('Network error', 'error'))
  })

  it('shows an error alert instead of "Loading…" when getFull rejects', async () => {
    const { muesli } = await import('@/api')
    vi.mocked(muesli.getFull).mockRejectedValueOnce(new Error('Network error'))

    render(<NoteScreen />)

    // Wait for the error state to appear
    const alert = await screen.findByRole('alert')
    expect(alert).toBeInTheDocument()
    expect(alert).toHaveTextContent('Could not load note')
    // "Loading…" should not be present
    expect(screen.queryByText('Loading…')).not.toBeInTheDocument()
  })

  it('re-calls getFull when the Retry button is clicked', async () => {
    const { muesli } = await import('@/api')
    // First call rejects, second call resolves normally
    vi.mocked(muesli.getFull)
      .mockRejectedValueOnce(new Error('Network error'))
      .mockResolvedValueOnce(fullNote)

    const user = userEvent.setup()
    render(<NoteScreen />)

    // Wait for error state
    const retryBtn = await screen.findByRole('button', { name: /retry/i })
    expect(retryBtn).toBeInTheDocument()

    // Click Retry — clears loadError and increments retryCount
    await user.click(retryBtn)

    // After retry, getFull should have been called again and the note renders
    await waitFor(() => expect(muesli.getFull).toHaveBeenCalledTimes(2))
    // Error state should be gone after successful retry
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
  })
})

describe('NoteScreen — loading skeleton', () => {
  it('shows note-loading skeleton while getFull is pending', async () => {
    const { muesli } = await import('@/api')
    // Make getFull never resolve so the component stays in the loading state
    vi.mocked(muesli.getFull).mockImplementationOnce(() => new Promise<FullNote>(() => {}))
    render(<NoteScreen />)
    // On initial render full===null, the skeleton should be present immediately
    expect(screen.getByTestId('note-loading')).toBeInTheDocument()
  })
})

// SRC01b — jump-to-timestamp: a search hit's `?segment=` param must reach
// NoteView as `initialSegmentId` (which resolves it to a highlightIndex; see
// NoteView.test.tsx for that half of the contract).
describe('NoteScreen — segment query param (SRC01b jump-to-timestamp)', () => {
  it("reads the 'segment' search param and passes it to NoteView as initialSegmentId", async () => {
    render(<NoteScreen />)
    expect(await screen.findByTestId('initial-segment-id')).toHaveTextContent('seg-99')
  })
})

describe('NoteScreen — Move to Trash dialog copy (EXT04)', () => {
  it('shows privacy-forward clarifying copy about exported files in the delete dialog', async () => {
    const user = userEvent.setup()
    render(<NoteScreen />)
    // Wait for the note to load so the header renders
    await screen.findByText('start-rec')
    // Click the delete-note button exposed by the mocked NoteHeader
    await user.click(screen.getByText('delete-note'))
    // The dialog should now be open — assert the new copy is present
    expect(screen.getByText(/Any files you've previously exported/)).toBeInTheDocument()
  })
})

// TPL01 — regenerateTemplate wiring: NoteScreen calls muesli.regenerateSummary
// with (noteId, templateId), then re-fetches + re-polls, disabling the control
// for that template while in flight and clearing it once the job settles.
describe('NoteScreen — regenerateTemplate (TPL01)', () => {
  it('calls muesli.regenerateSummary with (noteId, templateId) when Regenerate is clicked', async () => {
    const user = userEvent.setup()
    const { muesli } = await import('@/api')
    render(<NoteScreen />)
    const btn = await screen.findByText('regenerate-tpl-1')
    await user.click(btn)
    await waitFor(() => expect(muesli.regenerateSummary).toHaveBeenCalledWith('n1', 'tpl-1'))
  })

  it('re-fetches the note and re-engages polling after a successful regenerate', async () => {
    const user = userEvent.setup()
    const { muesli } = await import('@/api')
    render(<NoteScreen />)
    const btn = await screen.findByText('regenerate-tpl-1')
    const callsBefore = vi.mocked(muesli.getFull).mock.calls.length
    await user.click(btn)
    await waitFor(() => expect(vi.mocked(muesli.getFull).mock.calls.length).toBeGreaterThan(callsBefore))
  })

  it('disables the regenerate control for its template while in flight, then clears it', async () => {
    const user = userEvent.setup()
    const { muesli } = await import('@/api')
    let resolveRegen: () => void = () => {}
    vi.mocked(muesli.regenerateSummary).mockReturnValueOnce(new Promise((res) => { resolveRegen = () => res(undefined) }))
    render(<NoteScreen />)
    const btn = await screen.findByText('regenerate-tpl-1')
    await user.click(btn)

    // While the regenerateSummary call is unresolved, the mock NoteView is told
    // this template is regenerating (surfaced via the disabled attribute + the
    // regeneratingTemplateId span the test mock renders).
    await waitFor(() => expect(screen.getByTestId('regenerating-template-id')).toHaveTextContent('tpl-1'))
    expect(btn).toBeDisabled()

    resolveRegen()
    await waitFor(() => expect(screen.getByTestId('regenerating-template-id')).toHaveTextContent(''))
    expect(btn).not.toBeDisabled()
  })

  it('surfaces an error toast and clears the in-flight state if regenerateSummary rejects', async () => {
    const user = userEvent.setup()
    const { muesli } = await import('@/api')
    vi.mocked(muesli.regenerateSummary).mockRejectedValueOnce(new Error('boom'))
    render(<NoteScreen />)
    const btn = await screen.findByText('regenerate-tpl-1')
    await user.click(btn)
    await waitFor(() => expect(mockNotify).toHaveBeenCalledWith('boom', 'error'))
    await waitFor(() => expect(screen.getByTestId('regenerating-template-id')).toHaveTextContent(''))
  })
})
