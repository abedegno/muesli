// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { muesli } from '@/api'

vi.mock('@/api', () => ({ muesli: { updateTitle: vi.fn(), getCalendarEvents: vi.fn(async () => []) } }))

import { NoteHeader } from './NoteHeader'

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

function renderHeader(
  onDeleteNote = vi.fn(),
  onExport = vi.fn(),
  onDuplicate = vi.fn(),
  onResummarize?: () => void,
  onRetranscribe?: (options: { model?: string; language?: string }) => Promise<void>,
  serverExportFormats?: Array<{ id: string; label: string; onSelect: () => void }>,
) {
  const props = {
    noteId: 'n1',
    title: 'Sprint planning',
    recordState: 'idle' as const,
    elapsedMs: 0,
    onStart: vi.fn(),
    onStop: vi.fn(),
    onTitleSaved: vi.fn(),
    onDeleteNote,
    onDuplicate,
    onExport,
    pinned: false,
    onTogglePinned: vi.fn(),
    onResummarize,
    onRetranscribe,
    serverExportFormats,
  }
  const utils = render(<NoteHeader {...props} />)
  return { ...utils, props, onDeleteNote, onExport, onDuplicate, onResummarize }
}

describe('NoteHeader', () => {
  it('syncs the title input when the title prop changes externally and the field is not dirty', () => {
    const { rerender, props } = renderHeader()
    const input = screen.getByLabelText('Note title') as HTMLInputElement
    expect(input.value).toBe('Sprint planning')
    rerender(<NoteHeader {...props} title="Renamed remotely" />)
    expect(input.value).toBe('Renamed remotely')
  })

  it('does not clobber a local in-progress edit when the title prop changes externally', async () => {
    const user = userEvent.setup()
    const { rerender, props } = renderHeader()
    const input = screen.getByLabelText('Note title') as HTMLInputElement

    await user.clear(input)
    await user.type(input, 'Draft title')

    rerender(<NoteHeader {...props} title="Renamed remotely" />)

    expect(input.value).toBe('Draft title')
  })

  it('shows an inline error when updateTitle rejects and keeps the typed value', async () => {
    const user = userEvent.setup()
    vi.mocked(muesli.updateTitle).mockRejectedValueOnce(new Error('save failed'))
    const { props } = renderHeader()
    const input = screen.getByLabelText('Note title') as HTMLInputElement

    await user.clear(input)
    await user.type(input, 'Draft title')
    fireEvent.blur(input)

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/could not save title/i))
    expect(props.onTitleSaved).not.toHaveBeenCalled()
    expect(input).toHaveValue('Draft title')
  })

  it('pressing Enter in the title input commits the edit by blurring the field', async () => {
    const user = userEvent.setup()
    vi.mocked(muesli.updateTitle).mockResolvedValueOnce(undefined)
    renderHeader()
    const input = screen.getByLabelText('Note title') as HTMLInputElement

    await user.clear(input)
    await user.type(input, 'Draft title')
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(muesli.updateTitle).toHaveBeenCalledWith('n1', 'Draft title'))
    expect(muesli.updateTitle).toHaveBeenCalledTimes(1)
  })

  it('pressing Escape reverts the input value and does not call updateTitle', async () => {
    const user = userEvent.setup()
    renderHeader()
    const input = screen.getByLabelText('Note title') as HTMLInputElement

    await user.clear(input)
    await user.type(input, 'Draft title')
    fireEvent.keyDown(input, { key: 'Escape' })

    expect(input).toHaveValue('Sprint planning')
    expect(muesli.updateTitle).not.toHaveBeenCalled()
  })

  it('renders the overflow actions button', () => {
    renderHeader()
    expect(screen.getByRole('button', { name: /note actions/i })).toBeInTheDocument()
  })

  it('shows the Move to Trash item only after opening the menu', async () => {
    renderHeader()
    expect(screen.queryByRole('menuitem', { name: /move to trash/i })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    expect(screen.getByRole('menuitem', { name: /move to trash/i })).toBeInTheDocument()
  })

  it('calls onDeleteNote when Move to Trash is clicked', async () => {
    const { onDeleteNote } = renderHeader()
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    await userEvent.click(screen.getByRole('menuitem', { name: /move to trash/i }))
    expect(onDeleteNote).toHaveBeenCalledTimes(1)
  })

  it('shows Export as Markdown and calls onExport when clicked', async () => {
    const { onExport } = renderHeader()
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    const exportBtn = screen.getByRole('menuitem', { name: /export as markdown/i })
    expect(exportBtn).toBeInTheDocument()
    await userEvent.click(exportBtn)
    expect(onExport).toHaveBeenCalledTimes(1)
    // Clicking it closes the menu.
    expect(screen.queryByRole('menuitem', { name: /export as markdown/i })).not.toBeInTheDocument()
  })

  it('calls onDuplicate when Duplicate is clicked', async () => {
    const { onDuplicate } = renderHeader()
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    const duplicateBtn = screen.getByRole('menuitem', { name: /duplicate/i })
    expect(duplicateBtn).toBeInTheDocument()
    await userEvent.click(duplicateBtn)
    expect(onDuplicate).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('menuitem', { name: /duplicate/i })).not.toBeInTheDocument()
  })

  it('shows a server export submenu and runs the selected format', async () => {
    const onSelect = vi.fn()
    renderHeader(vi.fn(), vi.fn(), vi.fn(), undefined, undefined, [
      { id: 'md', label: 'Markdown', onSelect },
      { id: 'txt', label: 'Plain Text', onSelect },
      { id: 'docx', label: 'Word', onSelect },
    ])
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))

    const exportTrigger = screen.getByRole('menuitem', { name: /^export$/i })
    await userEvent.click(exportTrigger)

    const markdown = await screen.findByRole('menuitem', { name: /^markdown…$/i })
    expect(markdown).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /^plain text…$/i })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /^word…$/i })).toBeInTheDocument()
    await userEvent.click(markdown)
    expect(onSelect).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('menuitem', { name: /^markdown…$/i })).not.toBeInTheDocument()
  })

  it('shows an Export with options action in the server export submenu', async () => {
    const onExportWithOptions = vi.fn()
    render(<NoteHeader
      noteId="n1"
      title="Sprint planning"
      recordState="idle"
      elapsedMs={0}
      onStart={vi.fn()}
      onStop={vi.fn()}
      onTitleSaved={vi.fn()}
      onDeleteNote={vi.fn()}
      onDuplicate={vi.fn()}
      onExport={vi.fn()}
      pinned={false}
      onTogglePinned={vi.fn()}
      onExportWithOptions={onExportWithOptions}
      serverExportFormats={[
        { id: 'md', label: 'Markdown', onSelect: vi.fn() },
      ]}
    />)
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    await userEvent.click(screen.getByRole('menuitem', { name: /^export$/i }))
    await userEvent.click(await screen.findByRole('menuitem', { name: /export with options/i }))
    expect(onExportWithOptions).toHaveBeenCalledTimes(1)
  })

  it('closes the menu when Escape is pressed', async () => {
    renderHeader()
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    expect(screen.getByRole('menuitem', { name: /move to trash/i })).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('menuitem', { name: /move to trash/i })).not.toBeInTheDocument()
  })

  it('closes the menu on an outside click', async () => {
    renderHeader()
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    expect(screen.getByRole('menuitem', { name: /move to trash/i })).toBeInTheDocument()
    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('menuitem', { name: /move to trash/i })).not.toBeInTheDocument()
  })

  it('does not show Re-run summary when onResummarize is not provided', async () => {
    renderHeader()
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    expect(screen.queryByRole('menuitem', { name: /re-run summary/i })).not.toBeInTheDocument()
  })

  it('shows a pin toggle in the menu and labels it according to the current state', async () => {
    const onTogglePinned = vi.fn()
    const { unmount, props } = renderHeader()
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    expect(screen.getByRole('menuitem', { name: /pin note/i })).toBeInTheDocument()
    unmount()
    render(<NoteHeader {...props} pinned onTogglePinned={onTogglePinned} />)
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    expect(screen.getByRole('menuitem', { name: /unpin note/i })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('menuitem', { name: /unpin note/i }))
    expect(onTogglePinned).toHaveBeenCalledTimes(1)
  })

  it('shows Re-run summary when onResummarize is provided and calls it when clicked', async () => {
    const onResummarize = vi.fn()
    renderHeader(vi.fn(), vi.fn(), vi.fn(), onResummarize)
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    const reRunBtn = screen.getByRole('menuitem', { name: /re-run summary/i })
    expect(reRunBtn).toBeInTheDocument()
    await userEvent.click(reRunBtn)
    expect(onResummarize).toHaveBeenCalledTimes(1)
    // Clicking it closes the menu.
    expect(screen.queryByRole('menuitem', { name: /re-run summary/i })).not.toBeInTheDocument()
  })

  it('disables Re-run summary with a visible reason when no agent is configured', async () => {
    const onResummarize = vi.fn()
    const { props, unmount } = renderHeader(vi.fn(), vi.fn(), vi.fn(), onResummarize)
    unmount()
    render(<NoteHeader {...props} onResummarize={onResummarize} agentConfigured={false} />)
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    const reRunBtn = screen.getByRole('menuitem', { name: /re-run summary/i })
    expect(reRunBtn).toBeDisabled()
    expect(reRunBtn).toHaveTextContent('Agent plugin required')
    expect(reRunBtn).toHaveAttribute('title', 'AI features need an agent plugin configured.')
  })

  it('opens the enhance dialog and calls onRetranscribe with only the filled overrides', async () => {
    const user = userEvent.setup()
    const onRetranscribe = vi.fn().mockResolvedValue(undefined)
    renderHeader(vi.fn(), vi.fn(), vi.fn(), undefined, onRetranscribe)

    await user.click(screen.getByRole('button', { name: /note actions/i }))
    await user.click(screen.getByRole('menuitem', { name: /enhance/i }))

    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent('Re-transcribe note')

    await user.type(screen.getByLabelText(/model override/i), '  gpt-4o-mini  ')
    await user.type(screen.getByLabelText(/language override/i), ' en ')
    await user.click(screen.getByRole('button', { name: /^re-transcribe$/i }))

    await waitFor(() => expect(onRetranscribe).toHaveBeenCalledWith({ model: 'gpt-4o-mini', language: 'en' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  // --- a11y / keyboard hardening ---

  it('reflects aria-expanded and exposes aria-haspopup on the toggle', async () => {
    renderHeader()
    const toggle = screen.getByRole('button', { name: /note actions/i })
    expect(toggle).toHaveAttribute('aria-haspopup', 'menu')
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await userEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
  })

  it('exposes a menu role with menuitem children when open', async () => {
    renderHeader(vi.fn(), vi.fn(), vi.fn())
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    expect(screen.getByRole('menu', { name: /note actions/i })).toBeInTheDocument()
    expect(screen.getAllByRole('menuitem')).toHaveLength(4)
  })

  it('moves focus to the first item on open', async () => {
    renderHeader(vi.fn(), vi.fn(), vi.fn())
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    const items = screen.getAllByRole('menuitem')
    await waitFor(() => expect(document.activeElement).toBe(items[0]))
  })

  it('ArrowDown / ArrowUp move focus between items (wrapping)', async () => {
    renderHeader(vi.fn(), vi.fn(), vi.fn())
    await userEvent.click(screen.getByRole('button', { name: /note actions/i }))
    const items = screen.getAllByRole('menuitem')
    await waitFor(() => expect(document.activeElement).toBe(items[0]))

    fireEvent.keyDown(items[0], { key: 'ArrowDown' })
    expect(document.activeElement).toBe(items[1])

    fireEvent.keyDown(items[1], { key: 'ArrowDown' })
    expect(document.activeElement).toBe(items[2])

    fireEvent.keyDown(items[2], { key: 'ArrowDown' })
    expect(document.activeElement).toBe(items[3])

    // Wrap forward from last to first.
    fireEvent.keyDown(items[3], { key: 'ArrowDown' })
    expect(document.activeElement).toBe(items[0])

    // Wrap backward from first to last.
    fireEvent.keyDown(items[0], { key: 'ArrowUp' })
    expect(document.activeElement).toBe(items[3])
  })

  it('returns focus to the toggle when closed via Escape', async () => {
    renderHeader(vi.fn(), vi.fn(), vi.fn())
    const toggle = screen.getByRole('button', { name: /note actions/i })
    await userEvent.click(toggle)
    await waitFor(() => expect(document.activeElement).toBe(screen.getAllByRole('menuitem')[0]))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(document.activeElement).toBe(toggle)
  })

  it('returns focus to the toggle when closed via outside click', async () => {
    renderHeader(vi.fn(), vi.fn(), vi.fn())
    const toggle = screen.getByRole('button', { name: /note actions/i })
    await userEvent.click(toggle)
    await waitFor(() => expect(document.activeElement).toBe(screen.getAllByRole('menuitem')[0]))
    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(document.activeElement).toBe(toggle)
  })

  // --- autoFocusTitle ---

  it('focuses the title input on mount when autoFocusTitle is true', () => {
    const props = {
      noteId: 'n1',
      title: 'Meeting — Jun 28, 3:00 PM',
      recordState: 'idle' as const,
      elapsedMs: 0,
      onStart: vi.fn(),
      onStop: vi.fn(),
      onTitleSaved: vi.fn(),
      onDeleteNote: vi.fn(),
      onDuplicate: vi.fn(),
      onExport: vi.fn(),
      pinned: false,
      onTogglePinned: vi.fn(),
      autoFocusTitle: true,
    }
    render(<NoteHeader {...props} />)
    const input = screen.getByLabelText('Note title')
    expect(document.activeElement).toBe(input)
  })

  it('does not focus the title input on mount when autoFocusTitle is false', () => {
    const props = {
      noteId: 'n1',
      title: 'Sprint planning',
      recordState: 'idle' as const,
      elapsedMs: 0,
      onStart: vi.fn(),
      onStop: vi.fn(),
      onTitleSaved: vi.fn(),
      onDeleteNote: vi.fn(),
      onDuplicate: vi.fn(),
      onExport: vi.fn(),
      pinned: false,
      onTogglePinned: vi.fn(),
      autoFocusTitle: false,
    }
    render(<NoteHeader {...props} />)
    const input = screen.getByLabelText('Note title')
    expect(document.activeElement).not.toBe(input)
  })
})

describe('NoteHeader calendar link (CALLNK02)', () => {
  const baseProps = {
    noteId: 'n1',
    title: 'Sprint planning',
    recordState: 'idle' as const,
    elapsedMs: 0,
    onStart: vi.fn(),
    onStop: vi.fn(),
    onTitleSaved: vi.fn(),
    onDeleteNote: vi.fn(),
    onDuplicate: vi.fn(),
    onExport: vi.fn(),
    pinned: false,
    onTogglePinned: vi.fn(),
  }

  it('does not render the link-to-event affordance when the callbacks are not supplied', () => {
    render(<NoteHeader {...baseProps} />)
    expect(screen.queryByRole('button', { name: /link to calendar event/i })).not.toBeInTheDocument()
  })

  it('renders the link-to-event affordance when onLinkEvent/onUnlinkEvent are supplied', () => {
    render(<NoteHeader {...baseProps} onLinkEvent={vi.fn()} onUnlinkEvent={vi.fn()} />)
    expect(screen.getByRole('button', { name: /link to calendar event/i })).toBeInTheDocument()
  })

  it('shows the unlink affordance instead once eventId is set', async () => {
    render(<NoteHeader {...baseProps} eventId="evt-1" onLinkEvent={vi.fn()} onUnlinkEvent={vi.fn()} />)
    expect(screen.queryByRole('button', { name: /link to calendar event/i })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /unlink calendar event/i })).toBeInTheDocument()
    // Let the linked-event detail lookup (async) settle before the test ends.
    await waitFor(() => expect(screen.getByText(/linked event/i)).toBeInTheDocument())
  })
})
