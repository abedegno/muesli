// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { CommandPalette } from './CommandPalette'
import type { Note, Folder, SmartList } from '../../shared/types'

const NOTES: Note[] = [
  { id: '1', title: 'Standup', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
  { id: '2', title: 'Budget review', status: 'ready', created_at: '', updated_at: '', partial_transcript: false },
]
const FOLDERS: Folder[] = [{ id: 'f1', name: 'Projects', created_at: '' }]
const LISTS: SmartList[] = [{ id: 'l1', name: 'Recurring', rule: { op: 'and', children: [] }, created_at: '' }]
const TAGS = [{ name: 'design' }]

function setup(overrides: Partial<Parameters<typeof CommandPalette>[0]> = {}) {
  const onClose = vi.fn()
  const onSelectNote = vi.fn()
  const onSelectView = vi.fn()
  render(
    <CommandPalette
      open
      onClose={onClose}
      notes={NOTES}
      folders={FOLDERS}
      lists={LISTS}
      tags={TAGS}
      onSelectNote={onSelectNote}
      onSelectView={onSelectView}
      {...overrides}
    />,
  )
  return { onClose, onSelectNote, onSelectView }
}

afterEach(cleanup)

function ControlledPalette() {
  const [open, setOpen] = useState(false)
  return (
    <div>
      <button type="button" onClick={() => setOpen(true)}>Open palette</button>
      <CommandPalette
        open={open}
        onClose={() => setOpen(false)}
        notes={NOTES}
        folders={FOLDERS}
        lists={LISTS}
        tags={TAGS}
        onSelectNote={() => {}}
        onSelectView={() => {}}
      />
    </div>
  )
}

describe('CommandPalette', () => {
  it('moves focus into the search input when opened', async () => {
    const user = userEvent.setup()
    render(<ControlledPalette />)

    await user.click(screen.getByRole('button', { name: 'Open palette' }))

    await waitFor(() => expect(screen.getByLabelText('Search')).toHaveFocus())
  })

  it('shows recent note titles and "All notes" on an empty query', () => {
    setup()
    expect(screen.getByText('All notes')).toBeInTheDocument()
    expect(screen.getByText('Standup')).toBeInTheDocument()
    expect(screen.getByText('Budget review')).toBeInTheDocument()
  })

  it('renders nothing when closed', () => {
    const { onClose } = setup({ open: false })
    expect(screen.queryByLabelText('Search')).not.toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()
  })

  it('filters to a matching note and selecting it calls onSelectNote + onClose', async () => {
    const { onSelectNote, onClose } = setup()
    await userEvent.type(screen.getByLabelText('Search'), 'budget')
    expect(screen.queryByText('Standup')).not.toBeInTheDocument()
    await userEvent.click(screen.getByText('Budget review'))
    expect(onSelectNote).toHaveBeenCalledWith('2')
    expect(onClose).toHaveBeenCalled()
  })

  it('selecting a folder result calls onSelectView with folder view', async () => {
    const { onSelectView } = setup()
    await userEvent.type(screen.getByLabelText('Search'), 'projects')
    await userEvent.click(screen.getByText('Projects'))
    expect(onSelectView).toHaveBeenCalledWith({ type: 'folder', id: 'f1' })
  })

  it('selecting a list result calls onSelectView with list view', async () => {
    const { onSelectView } = setup()
    await userEvent.type(screen.getByLabelText('Search'), 'recurring')
    await userEvent.click(screen.getByText('Recurring'))
    expect(onSelectView).toHaveBeenCalledWith({ type: 'list', id: 'l1' })
  })

  it('labels tag results with a # prefix and selecting calls onSelectView with tag view', async () => {
    const { onSelectView } = setup()
    await userEvent.type(screen.getByLabelText('Search'), 'design')
    await userEvent.click(screen.getByText('#design'))
    expect(onSelectView).toHaveBeenCalledWith({ type: 'tag', tag: 'design' })
  })

  it('Escape on the input calls onClose', async () => {
    const { onClose } = setup()
    const input = screen.getByLabelText('Search')
    input.focus()
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalled()
  })

  it('marks the floating panel as a modal dialog', () => {
    setup()
    const dialog = screen.getByRole('dialog', { name: /command palette/i })
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    // The dialog role is on the panel containing the input, not the backdrop.
    expect(dialog).toContainElement(screen.getByLabelText('Search'))
  })

  it('Tab wraps from the last focusable element back to the first', async () => {
    const user = userEvent.setup()
    setup()
    const input = screen.getByLabelText('Search')
    await waitFor(() => expect(input).toHaveFocus())

    await user.type(input, 'budget')
    const last = screen.getByRole('button', { name: 'Budget review' })
    last.focus()
    fireEvent.keyDown(last, { key: 'Tab' })

    expect(input).toHaveFocus()
  })

  it('Shift+Tab wraps from the first focusable element back to the last', async () => {
    const user = userEvent.setup()
    setup()
    const input = screen.getByLabelText('Search')
    await waitFor(() => expect(input).toHaveFocus())

    await user.type(input, 'budget')
    const last = screen.getByRole('button', { name: 'Budget review' })
    fireEvent.keyDown(input, { key: 'Tab', shiftKey: true })

    expect(last).toHaveFocus()
  })

  it('restores focus to the previously focused element when the palette closes', async () => {
    const user = userEvent.setup()
    render(<ControlledPalette />)
    const opener = screen.getByRole('button', { name: 'Open palette' })
    opener.focus()

    await user.click(opener)
    await waitFor(() => expect(screen.getByLabelText('Search')).toHaveFocus())

    await user.keyboard('{Escape}')

    await waitFor(() => expect(opener).toHaveFocus())
  })

  it('ArrowUp from the first item wraps to the last item', async () => {
    // Empty query order: [All notes, Standup, Budget review].
    // active starts at 0; ArrowUp wraps to the last item (Budget review).
    const { onSelectNote } = setup()
    const input = screen.getByLabelText('Search')
    input.focus()
    await userEvent.keyboard('{ArrowUp}{Enter}')
    expect(onSelectNote).toHaveBeenCalledWith('2')
  })

  it('ArrowDown then Enter activates the second item', async () => {
    // Empty query order: [All notes, Standup, Budget review].
    // ArrowDown moves active from 0 -> 1 (Standup), Enter selects it.
    const { onSelectNote, onSelectView } = setup()
    const input = screen.getByLabelText('Search')
    input.focus()
    await userEvent.keyboard('{ArrowDown}{Enter}')
    expect(onSelectNote).toHaveBeenCalledWith('1')
    expect(onSelectView).not.toHaveBeenCalled()
  })

  describe('folder affordance', () => {
    it('shows "→ folder" badge next to folder results', async () => {
      setup()
      await userEvent.type(screen.getByLabelText('Search'), 'projects')
      // The folder label "Projects" should be visible
      expect(screen.getByText('Projects')).toBeInTheDocument()
      // The "→ folder" affordance badge should be visible for folder items
      expect(screen.getByText('→ folder')).toBeInTheDocument()
    })

    it('does NOT show "→ folder" badge next to note results', async () => {
      setup()
      await userEvent.type(screen.getByLabelText('Search'), 'standup')
      // The note "Standup" should be visible
      expect(screen.getByText('Standup')).toBeInTheDocument()
      // No "→ folder" affordance should appear for note results
      expect(screen.queryByText('→ folder')).not.toBeInTheDocument()
    })
  })

  describe('action commands', () => {
    it('shows action label under a "Commands" group on empty query', () => {
      const run = vi.fn()
      setup({ actions: [{ label: 'New meeting', run }] })
      expect(screen.getByText('New meeting')).toBeInTheDocument()
      expect(screen.getByText('Commands')).toBeInTheDocument()
    })

    it('clicking an action calls run and onClose', async () => {
      const run = vi.fn()
      const { onClose } = setup({ actions: [{ label: 'New meeting', run }] })
      await userEvent.click(screen.getByText('New meeting'))
      expect(run).toHaveBeenCalled()
      expect(onClose).toHaveBeenCalled()
    })

    it('shows action when query matches its label', async () => {
      const run = vi.fn()
      setup({ actions: [{ label: 'New meeting', run }] })
      await userEvent.type(screen.getByLabelText('Search'), 'new')
      expect(screen.getByText('New meeting')).toBeInTheDocument()
    })

    it('hides action when query does not match its label', async () => {
      const run = vi.fn()
      setup({ actions: [{ label: 'New meeting', run }] })
      await userEvent.type(screen.getByLabelText('Search'), 'budget')
      expect(screen.queryByText('New meeting')).not.toBeInTheDocument()
    })

    it('renders without actions prop (default empty)', () => {
      setup()
      expect(screen.queryByText('COMMANDS')).not.toBeInTheDocument()
    })
  })
})
