// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { PersonDetailScreen } from './PersonDetailScreen'
import type { Note, PersonWithCompany } from '../../shared/types'

const { getPersonMock, getPersonNotesMock, navigateMock, paramsState } = vi.hoisted(() => ({
  getPersonMock: vi.fn(),
  getPersonNotesMock: vi.fn(),
  navigateMock: vi.fn(),
  paramsState: { id: 'p1' as string },
}))

vi.mock('react-router-dom', () => ({
  useNavigate: () => navigateMock,
  useParams: () => paramsState,
  Link: ({ to, children, ...props }: { to: string; children: ReactNode }) => (
    <a href={to} {...props}>{children}</a>
  ),
}))

vi.mock('@/api', () => ({
  muesli: {
    getPerson: (...args: Parameters<typeof getPersonMock>) => getPersonMock(...args),
    getPersonNotes: (...args: Parameters<typeof getPersonNotesMock>) => getPersonNotesMock(...args),
  },
}))

afterEach(cleanup)

beforeEach(() => {
  getPersonMock.mockReset()
  getPersonNotesMock.mockReset()
  navigateMock.mockReset()
  paramsState.id = 'p1'
})

const person = (over: Partial<PersonWithCompany> = {}): PersonWithCompany => ({
  id: 'p1',
  primary_email: 'alex@example.com',
  display_name: 'Alex Doe',
  first_seen_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-02T00:00:00Z',
  company: {
    id: 'c1',
    owner_id: 'owner-1',
    domain: 'example.com',
    name: 'Example Inc',
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-02T00:00:00Z',
  },
  ...over,
})

const note = (over: Partial<Note> = {}): Note => ({
  id: 'n1',
  title: 'Weekly sync',
  status: 'ready',
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  partial_transcript: false,
  started_at: '2026-07-01T00:00:00Z',
  ...over,
})
const noteDate = new Date('2026-07-01T00:00:00Z').toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })

describe('PersonDetailScreen', () => {
  it('renders the person details and notes', async () => {
    getPersonMock.mockResolvedValue(person())
    getPersonNotesMock.mockResolvedValue([note()])

    render(<PersonDetailScreen />)

    expect(await screen.findByText('Alex Doe')).toBeInTheDocument()
    expect(screen.getByText('alex@example.com')).toBeInTheDocument()
    expect(screen.getByText('Example Inc')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: `Weekly sync ${noteDate}` })).toHaveAttribute('href', '/notes/n1')

    await userEvent.click(screen.getByRole('button', { name: /back to people/i }))
    expect(navigateMock).toHaveBeenCalledWith('/people')
  })

  it('shows the empty state when there are no notes', async () => {
    getPersonMock.mockResolvedValue(person())
    getPersonNotesMock.mockResolvedValue([])

    render(<PersonDetailScreen />)

    expect(await screen.findByText('No notes yet')).toBeInTheDocument()
  })

  it('shows an error block when loading fails', async () => {
    getPersonMock.mockResolvedValue(person())
    getPersonNotesMock.mockRejectedValue(new Error('notes offline'))

    render(<PersonDetailScreen />)

    expect(await screen.findByText(/could not load person/i)).toBeInTheDocument()
    expect(screen.getByText('notes offline')).toBeInTheDocument()
  })
})
