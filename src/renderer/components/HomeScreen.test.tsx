// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Outlet, Route, Routes, useParams } from 'react-router-dom'
import { HomeScreen } from './HomeScreen'
import type { Folder, Note } from '../../shared/types'

const { getCalendarEventsMock } = vi.hoisted(() => ({
  getCalendarEventsMock: vi.fn(),
}))

vi.mock('@/api', () => ({
  muesli: {
    getCalendarEvents: getCalendarEventsMock,
  },
}))

afterEach(cleanup)
beforeEach(() => {
  getCalendarEventsMock.mockReset()
  getCalendarEventsMock.mockResolvedValue([])
})

function NoteRouteStub() {
  const { id } = useParams()
  return <div data-testid="note-route">{id}</div>
}

function OutletStub({
  allNotes,
  folders = [],
  refresh = () => {},
  loaded = true,
}: {
  allNotes: Note[]
  folders?: Folder[]
  refresh?: () => void
  loaded?: boolean
}) {
  return (
    <Outlet
      context={{
        allNotes,
        folders,
        refresh,
        loaded,
      }}
    />
  )
}

function renderHome(allNotes: Note[]) {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route element={<OutletStub allNotes={allNotes} />}>
          <Route path="/" element={<HomeScreen />} />
          <Route path="/notes/:id" element={<NoteRouteStub />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  )
}

const note = (over: Partial<Note>): Note => ({
  id: 'n1',
  title: 'Standup',
  status: 'ready',
  created_at: new Date().toISOString(),
  updated_at: '',
  partial_transcript: false,
  ...over,
})

describe('HomeScreen', () => {
  it('groups recent notes by date and links each note to the matching note route', async () => {
    const today = new Date()
    const yesterday = new Date(today.getTime() - 86_400_000)
    renderHome([
      note({ id: 'n1', title: 'Today note', created_at: today.toISOString() }),
      note({ id: 'n2', title: 'Yesterday note', created_at: yesterday.toISOString() }),
    ])

    await screen.findByText('Recent notes')
    expect(screen.getByRole('heading', { name: 'Today' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Yesterday' })).toBeInTheDocument()

    const user = userEvent.setup()
    await user.click(screen.getByText('Yesterday note').closest('button')!)

    await waitFor(() => expect(screen.getByTestId('note-route')).toHaveTextContent('n2'))
  })
})
