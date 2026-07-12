// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useParams } from 'react-router-dom'
import type { ActionItem, Note, PersonWithCompany } from '../../shared/types'
import { ActionItemsScreen } from './ActionItemsScreen'

const { listNotesMock, listPeopleMock, listActionItemsMock } = vi.hoisted(() => ({
  listNotesMock: vi.fn(),
  listPeopleMock: vi.fn(),
  listActionItemsMock: vi.fn(),
}))

vi.mock('@/api', () => ({
  muesli: {
    listNotes: () => listNotesMock(),
    listPeople: () => listPeopleMock(),
    listActionItems: () => listActionItemsMock(),
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

function person(over: Partial<PersonWithCompany> = {}): PersonWithCompany {
  return {
    id: 'person-1',
    primary_email: 'alex@example.com',
    display_name: 'Alex Kim',
    first_seen_at: '2026-06-30T10:00:00Z',
    updated_at: '2026-07-01T10:00:00Z',
    ...over,
  }
}

function actionItem(over: Partial<ActionItem> = {}): ActionItem {
  return {
    id: 'ai-1',
    note_id: 'note-a',
    owner_id: 'owner-1',
    text: 'Ship the doc',
    owner_person_id: 'person-1',
    status: 'open',
    due_hint: 'Friday',
    created_at: '2026-07-01T12:00:00Z',
    ...over,
  }
}

beforeEach(() => {
  listNotesMock.mockReset()
  listPeopleMock.mockReset()
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
    listPeopleMock.mockResolvedValue([
      person({ id: 'person-1', display_name: 'Alex Kim', primary_email: 'alex@example.com' }),
      person({ id: 'person-2', display_name: 'Brooke Park', primary_email: 'brooke@example.com' }),
    ])
    listActionItemsMock.mockResolvedValue([
      actionItem({ id: 'ai-1', note_id: 'note-a', text: 'Ship the doc', created_at: '2026-07-01T12:00:00Z', owner_person_id: 'person-1' }),
      actionItem({ id: 'ai-2', note_id: 'note-b', text: 'Book the follow-up', due_hint: '', created_at: '2026-07-02T12:00:00Z', owner_person_id: 'person-2' }),
      actionItem({ id: 'ai-3', note_id: 'note-b', text: 'Send the recap', status: 'done', due_hint: '', created_at: '2026-07-02T13:00:00Z', owner_person_id: 'person-1' }),
      actionItem({ id: 'ai-4', note_id: 'note-a', text: 'Prep the agenda', due_hint: '', created_at: '2026-07-01T13:00:00Z', owner_person_id: null, owner_id: 'owner-2' }),
    ])
  })

  it('renders action items across notes and navigates to the source note route', async () => {
    const user = userEvent.setup()
    renderScreen()

    expect(await screen.findByRole('link', { name: 'Planning review' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Customer sync' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /ship the doc/i })).toHaveAttribute('href', '/notes/note-a')
    expect(screen.queryByText('Send the recap')).not.toBeInTheDocument()
    expect(screen.getByText('3 open - 1 done')).toBeInTheDocument()

    await user.click(screen.getByRole('link', { name: /ship the doc/i }))
    expect(await screen.findByText('Note route: note-a')).toBeInTheDocument()
  })

  it('filters action items by owner without showing non-matching rows', async () => {
    const user = userEvent.setup()
    renderScreen()

    expect(await screen.findByRole('link', { name: 'Planning review' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /ship the doc/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /book the follow-up/i })).toBeInTheDocument()

    await user.selectOptions(screen.getByLabelText('Filter action items by owner'), 'person-1')

    expect(screen.getByRole('link', { name: /ship the doc/i })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /book the follow-up/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /prep the agenda/i })).not.toBeInTheDocument()
  })

  it('updates the header count when the owner filter changes', async () => {
    const user = userEvent.setup()
    renderScreen()

    expect(await screen.findByRole('link', { name: 'Planning review' })).toBeInTheDocument()
    expect(screen.getByText('3 open - 1 done')).toBeInTheDocument()

    await user.selectOptions(screen.getByLabelText('Filter action items by owner'), 'person-1')

    await waitFor(() => {
      expect(screen.getByText('1 open - 1 done')).toBeInTheDocument()
    })
  })

  it('switches status filters client-side and reveals done items', async () => {
    const user = userEvent.setup()
    renderScreen()

    expect(await screen.findByRole('link', { name: 'Planning review' })).toBeInTheDocument()
    expect(listActionItemsMock).toHaveBeenCalledTimes(1)
    expect(listActionItemsMock.mock.calls[0]).toEqual([])

    await user.click(screen.getByRole('button', { name: 'Done' }))

    expect(screen.queryByRole('link', { name: /ship the doc/i })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: /send the recap/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Done' })).toHaveAttribute('aria-pressed', 'true')

    await user.click(screen.getByRole('button', { name: 'All' }))

    expect(screen.getByRole('link', { name: /prep the agenda/i })).toBeInTheDocument()
  })
})
