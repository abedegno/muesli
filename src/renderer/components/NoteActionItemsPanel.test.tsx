// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ActionItem, Decision } from '../../shared/types'
import { NoteActionItemsPanel } from './NoteActionItemsPanel'

const { listNoteActionItemsMock, updateActionItemMock } = vi.hoisted(() => ({
  listNoteActionItemsMock: vi.fn(),
  updateActionItemMock: vi.fn(),
}))

vi.mock('@/api', () => ({
  muesli: {
    listNoteActionItems: (...args: Parameters<typeof listNoteActionItemsMock>) => listNoteActionItemsMock(...args),
    updateActionItem: (...args: Parameters<typeof updateActionItemMock>) => updateActionItemMock(...args),
  },
}))

afterEach(cleanup)

beforeEach(() => {
  listNoteActionItemsMock.mockReset()
  updateActionItemMock.mockReset()
})

const actionItem = (over: Partial<ActionItem> = {}): ActionItem => ({
  id: 'ai-1',
  note_id: 'note-1',
  owner_id: 'owner-1',
  text: 'Ship the launch notes',
  owner_person_id: null,
  status: 'open',
  due_hint: 'Tomorrow',
  created_at: '2026-07-11T00:00:00Z',
  ...over,
})

const decision = (over: Partial<Decision> = {}): Decision => ({
  id: 'd-1',
  note_id: 'note-1',
  owner_id: 'owner-1',
  text: 'Use the weekly cadence',
  created_at: '2026-07-11T00:00:00Z',
  ...over,
})

describe('NoteActionItemsPanel', () => {
  it('renders action items and decisions from the bridge', async () => {
    listNoteActionItemsMock.mockResolvedValue({
      actionItems: [actionItem()],
      decisions: [decision()],
    })

    render(<NoteActionItemsPanel noteId="note-1" />)

    expect(await screen.findByText('Ship the launch notes')).toBeInTheDocument()
    expect(screen.getByText('Due: Tomorrow')).toBeInTheDocument()
    expect(screen.getByText('Use the weekly cadence')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Ship the launch notes' })).not.toBeChecked()
  })

  it('shows the empty state when there are no action items', async () => {
    listNoteActionItemsMock.mockResolvedValue({ actionItems: [], decisions: [] })

    render(<NoteActionItemsPanel noteId="note-1" />)

    expect(await screen.findByText('No action items')).toBeInTheDocument()
    expect(screen.getByText('No decisions')).toBeInTheDocument()
  })

  it('shows an error state when loading fails', async () => {
    listNoteActionItemsMock.mockRejectedValue(new Error('action items unavailable'))

    render(<NoteActionItemsPanel noteId="note-1" />)

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not load action items.')
    expect(screen.getByText('action items unavailable')).toBeInTheDocument()
    expect(screen.queryByText('No action items')).not.toBeInTheDocument()
  })

  it('toggles an action item via the bridge and updates the checkbox', async () => {
    const user = userEvent.setup()
    listNoteActionItemsMock.mockResolvedValue({
      actionItems: [actionItem()],
      decisions: [],
    })
    updateActionItemMock.mockResolvedValue(actionItem({ status: 'done' }))

    render(<NoteActionItemsPanel noteId="note-1" />)

    const checkbox = await screen.findByRole('checkbox', { name: 'Ship the launch notes' })
    await user.click(checkbox)

    await waitFor(() => expect(updateActionItemMock).toHaveBeenCalledWith('ai-1', { status: 'done' }))
    expect(checkbox).toBeChecked()
  })
})
