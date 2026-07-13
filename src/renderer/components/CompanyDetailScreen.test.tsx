// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { CompanyDetailScreen } from './CompanyDetailScreen'
import type { CompanyWithPeople, Note, Person } from '../../shared/types'

const { getCompanyMock, getPersonNotesMock, navigateMock, paramsState } = vi.hoisted(() => ({
  getCompanyMock: vi.fn(),
  getPersonNotesMock: vi.fn(),
  navigateMock: vi.fn(),
  paramsState: { id: 'c1' as string },
}))

vi.mock('react-router-dom', () => ({
  useNavigate: () => navigateMock,
  useParams: () => paramsState,
  Link: ({ to, children, ...props }: { to: string; children: ReactNode }) => (
    <a
      href={to}
      onClick={(event) => {
        event.preventDefault()
        navigateMock(to)
      }}
      {...props}
    >
      {children}
    </a>
  ),
}))

vi.mock('@/api', () => ({
  muesli: {
    getCompany: (...args: Parameters<typeof getCompanyMock>) => getCompanyMock(...args),
    getPersonNotes: (...args: Parameters<typeof getPersonNotesMock>) => getPersonNotesMock(...args),
  },
}))

afterEach(cleanup)

beforeEach(() => {
  getCompanyMock.mockReset()
  getPersonNotesMock.mockReset()
  navigateMock.mockReset()
  paramsState.id = 'c1'
})

const person = (over: Partial<Person> = {}): Person => ({
  id: 'p1',
  primary_email: 'alex@example.com',
  display_name: 'Alex Doe',
  company_id: 'c1',
  first_seen_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-02T00:00:00Z',
  ...over,
})

const company = (over: Partial<CompanyWithPeople> = {}): CompanyWithPeople => ({
  id: 'c1',
  owner_id: 'owner-1',
  domain: 'example.com',
  name: 'Example Inc',
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-02T00:00:00Z',
  people: [person()],
  ...over,
})

const note = (over: Partial<Note> = {}): Note => ({
  id: 'n1',
  title: 'Weekly sync',
  status: 'ready',
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  partial_transcript: false,
  started_at: '2026-07-01T09:00:00Z',
  ended_at: '2026-07-01T09:30:00Z',
  ...over,
})

describe('CompanyDetailScreen', () => {
  it('renders the company details and people', async () => {
    getCompanyMock.mockResolvedValue(company())

    render(<CompanyDetailScreen />)

    expect(await screen.findByText('Example Inc')).toBeInTheDocument()
    expect(screen.getByText('E')).toBeInTheDocument()
    expect(screen.getByText('example.com')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('link', { name: 'Alex Doe alex@example.com' }))
    expect(navigateMock).toHaveBeenCalledWith('/people/p1')

    await userEvent.click(screen.getByRole('button', { name: /back to people/i }))
    expect(navigateMock).toHaveBeenCalledWith('/people')
  })

  it('renders deduplicated activity rollup stats', async () => {
    const shared = note({ id: 'shared', started_at: '2026-07-01T09:00:00Z', ended_at: '2026-07-01T09:30:00Z', created_at: '2026-07-01T08:00:00Z' })
    const firstUnique = note({ id: 'n2', title: 'Planning', started_at: '2026-07-02T10:00:00Z', ended_at: '2026-07-02T11:00:00Z', created_at: '2026-07-02T09:00:00Z' })
    const secondUnique = note({ id: 'n3', title: 'Review', started_at: '2026-07-03T12:00:00Z', ended_at: '2026-07-03T13:00:00Z', created_at: '2026-07-03T11:00:00Z' })

    getCompanyMock.mockResolvedValue(company({
      people: [
        person({ id: 'p1' }),
        person({ id: 'p2', display_name: 'Bea Chen', primary_email: 'bea@example.com' }),
      ],
    }))
    getPersonNotesMock.mockImplementation((personId: string) => {
      if (personId === 'p1') {
        return Promise.resolve([shared, firstUnique])
      }
      return Promise.resolve([shared, secondUnique])
    })

    render(<CompanyDetailScreen />)

    const meetingsTile = await screen.findByText('Meetings')
    const hoursTile = screen.getByText('Hours')
    const lastSeenTile = screen.getByText('Last seen')

    expect(within(meetingsTile.closest('div') as HTMLElement).getByText('3')).toBeInTheDocument()
    expect(within(hoursTile.closest('div') as HTMLElement).getByText('2.5')).toBeInTheDocument()
    expect(within(lastSeenTile.closest('div') as HTMLElement).getByText(new Date('2026-07-03T12:00:00Z').toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' }))).toBeInTheDocument()
  })

  it('shows the empty state when there are no people', async () => {
    getCompanyMock.mockResolvedValue(company({ people: [] }))

    render(<CompanyDetailScreen />)

    expect(await screen.findByText('No people yet')).toBeInTheDocument()
  })

  it('shows an error block when loading fails', async () => {
    getCompanyMock.mockRejectedValue(new Error('company offline'))

    render(<CompanyDetailScreen />)

    expect(await screen.findByText(/could not load company/i)).toBeInTheDocument()
    expect(screen.getByText('company offline')).toBeInTheDocument()
  })
})
