// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useParams } from 'react-router-dom'
import type { ActionItem, Note } from '../../shared/types'
import { ActionItemsScreen } from './ActionItemsScreen'

const { listNotesMock, listActionItemsMock } = vi.hoisted(() => ({
  listNotesMock: vi.fn(),
  listActionItemsMock: vi.fn(),
}))

vi.mock('@/api', () => ({
  muesli: {
    listNotes: () => listNotesMock(),
    listActionItems: (status?: string) => listActionItemsMock(status),
  },
}))

function NoteRoute() {
  const { id = '' } = useParams()
  return <div>Note route: {id}</div>
}

function note(over: Partial<Note> = {}): Note {
  return {
    id: 'note-a',
    title: 'Planning review',
    status: 'ready',
    created_at: '2026-07-01T10:00:00Z',
    updated_at: '2026-07-01T11:00:00Z',
    partial_transcript: false,
    ...over,
  }
}

function actionItem(over: Partial<ActionItem> = {}): ActionItem {
  return {
    id: 'ai-1',
    note_id: 'note-a',
    owner_id: 'owner-1',
    text: 'Ship the doc',
    owner_person_id: null,
    status: 'open',
    due_hint: 'Friday',
    created_at: '2026-07-01T12:00:00Z',
    ...over,
  }
}

beforeEach(() => {
  listNotesMock.mockReset()
  listActionItemsMock.mockReset()
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('ActionItemsScreen', () => {
  function renderScreen() {
    return render(
      <MemoryRouter initialEntries={['/action-items']}>
        <Routes>
          <Route path="/action-items" element={<ActionItemsScreen />} />
          <Route path="/notes/:id" element={<NoteRoute />} />
        </Routes>
      </MemoryRouter>,
    )
  }

  beforeEach(() => {
    listNotesMock.mockResolvedValue([
      note({ id: 'note-a', title: 'Planning review', created_at: '2026-07-01T10:00:00Z' }),
      note({ id: 'note-b', title: 'Customer sync', created_at: '2026-07-02T10:00:00Z' }),
    ])
    listActionItemsMock.mockImplementation(async (status?: string) => {
      if (status === 'all') {
        return [
          actionItem({ id: 'ai-1', note_id: 'note-a', text: 'Ship the doc', created_at: '2026-07-01T12:00:00Z' }),
          actionItem({ id: 'ai-2', note_id: 'note-b', text: 'Book the follow-up', due_hint: '', created_at: '2026-07-02T12:00:00Z' }),
          actionItem({ id: 'ai-3', note_id: 'note-b', text: 'Send the recap', status: 'done', due_hint: '', created_at: '2026-07-02T13:00:00Z' }),
        ]
      }
      return [
        actionItem({ id: 'ai-1', note_id: 'note-a', text: 'Ship the doc', created_at: '2026-07-01T12:00:00Z' }),
        actionItem({ id: 'ai-2', note_id: 'note-b', text: 'Book the follow-up', due_hint: '', created_at: '2026-07-02T12:00:00Z' }),
      ]
    })
  })

  it('renders action items across notes and navigates to the source note route', async () => {
    const user = userEvent.setup()
    renderScreen()

    expect(await screen.findByText('Planning review')).toBeInTheDocument()
    expect(screen.getByText('Customer sync')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /ship the doc/i })).toHaveAttribute('href', '/notes/note-a')
    expect(screen.queryByText('Send the recap')).not.toBeInTheDocument()

    await user.click(screen.getByRole('link', { name: /ship the doc/i }))
    expect(await screen.findByText('Note route: note-a')).toBeInTheDocument()
  })

  it('refetches with status=all when the toggle changes', async () => {
    const user = userEvent.setup()
    renderScreen()

    expect(await screen.findByText('Planning review')).toBeInTheDocument()
    expect(listActionItemsMock).toHaveBeenCalledWith('open')

    await user.click(screen.getByRole('button', { name: 'All' }))

    await waitFor(() => {
      expect(listActionItemsMock).toHaveBeenLastCalledWith('all')
    })
    expect(await screen.findByText('Send the recap')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText('Done')).toBeInTheDocument()
  })
})
