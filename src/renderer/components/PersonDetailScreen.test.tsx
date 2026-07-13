// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { PersonDetailScreen } from './PersonDetailScreen'
import type { CompanyWithCount, Note, PersonWithCompany } from '../../shared/types'

const { deletePersonMock, getPersonMock, getPersonNotesMock, listCompaniesMock, listPeopleMock, mergePeopleMock, navigateMock, paramsState, updatePersonMock } = vi.hoisted(() => ({
  deletePersonMock: vi.fn(),
  getPersonMock: vi.fn(),
  getPersonNotesMock: vi.fn(),
  listCompaniesMock: vi.fn(),
  listPeopleMock: vi.fn(),
  mergePeopleMock: vi.fn(),
  navigateMock: vi.fn(),
  paramsState: { id: 'p1' as string },
  updatePersonMock: vi.fn(),
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
    deletePerson: (...args: Parameters<typeof deletePersonMock>) => deletePersonMock(...args),
    getPerson: (...args: Parameters<typeof getPersonMock>) => getPersonMock(...args),
    getPersonNotes: (...args: Parameters<typeof getPersonNotesMock>) => getPersonNotesMock(...args),
    listCompanies: (...args: Parameters<typeof listCompaniesMock>) => listCompaniesMock(...args),
    listPeople: (...args: Parameters<typeof listPeopleMock>) => listPeopleMock(...args),
    mergePeople: (...args: Parameters<typeof mergePeopleMock>) => mergePeopleMock(...args),
    updatePerson: (...args: Parameters<typeof updatePersonMock>) => updatePersonMock(...args),
  },
}))

afterEach(() => {
  cleanup()
})

afterEach(() => {
  vi.restoreAllMocks()
})

beforeEach(() => {
  deletePersonMock.mockReset()
  getPersonMock.mockReset()
  getPersonNotesMock.mockReset()
  listCompaniesMock.mockReset()
  listPeopleMock.mockReset()
  mergePeopleMock.mockReset()
  navigateMock.mockReset()
  updatePersonMock.mockReset()
  paramsState.id = 'p1'
})

const company = (over: Partial<CompanyWithCount> = {}): CompanyWithCount => ({
  id: 'c1',
  owner_id: 'owner-1',
  domain: 'example.com',
  name: 'Example Inc',
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-02T00:00:00Z',
  people_count: 3,
  ...over,
})

type PersonFixture = Omit<Partial<PersonWithCompany>, 'company'> & { company?: PersonWithCompany['company'] | null }

const person = (
  over: PersonFixture = {},
): PersonWithCompany => ({
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
}) as PersonWithCompany

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
const formatDate = (iso: string) => new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })

describe('PersonDetailScreen', () => {
  it('renders the person details and notes', async () => {
    getPersonMock.mockResolvedValue(person())
    getPersonNotesMock.mockResolvedValue([note()])
    listCompaniesMock.mockResolvedValue([company()])
    listPeopleMock.mockResolvedValue([person(), person({ id: 'p2', display_name: 'Other Person', primary_email: 'other@example.com' })])

    render(<PersonDetailScreen />)

    expect(await screen.findByText('Alex Doe')).toBeInTheDocument()
    expect(within(screen.getByRole('heading', { name: 'Alex Doe' }).closest('section') as HTMLElement).getByText('A')).toBeInTheDocument()
    expect(screen.getByText('alex@example.com')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('link', { name: 'Example Inc' }))
    expect(navigateMock).toHaveBeenCalledWith('/companies/c1')
    expect(screen.getByRole('link', { name: `Weekly sync ${noteDate}` })).toHaveAttribute('href', '/notes/n1')

    await userEvent.click(screen.getByRole('button', { name: /back to people/i }))
    expect(navigateMock).toHaveBeenCalledWith('/people')
  })

  it('renders the activity rollup stats', async () => {
    getPersonMock.mockResolvedValue(person())
    getPersonNotesMock.mockResolvedValue([
      note({
        id: 'n1',
        started_at: '2026-07-01T10:00:00Z',
        ended_at: '2026-07-01T11:30:00Z',
        created_at: '2026-07-01T09:00:00Z',
      }),
      note({
        id: 'n2',
        title: 'Design review',
        started_at: '2026-07-03T12:00:00Z',
        ended_at: '2026-07-03T13:00:00Z',
        created_at: '2026-07-03T08:00:00Z',
      }),
    ])
    listCompaniesMock.mockResolvedValue([company()])
    listPeopleMock.mockResolvedValue([person(), person({ id: 'p2', display_name: 'Other Person', primary_email: 'other@example.com' })])

    render(<PersonDetailScreen />)

    const meetingsTile = await screen.findByText('Meetings')
    const hoursTile = screen.getByText('Hours')
    const lastSeenTile = screen.getByText('Last seen')

    expect(within(meetingsTile.closest('div') as HTMLElement).getByText('2')).toBeInTheDocument()
    expect(within(hoursTile.closest('div') as HTMLElement).getByText('2.5')).toBeInTheDocument()
    expect(within(lastSeenTile.closest('div') as HTMLElement).getByText(formatDate('2026-07-03T12:00:00Z'))).toBeInTheDocument()
  })

  it('submits edits and updates the displayed name and company', async () => {
    getPersonMock.mockResolvedValue(person())
    getPersonNotesMock.mockResolvedValue([])
    listCompaniesMock.mockResolvedValue([
      company(),
      company({ id: 'c2', domain: 'acme.com', name: 'Acme Co', people_count: 1 }),
    ])
    listPeopleMock.mockResolvedValue([
      person(),
      person({ id: 'p2', display_name: 'Other Person', primary_email: 'other@example.com', company: null }),
    ])
    updatePersonMock.mockResolvedValue(person({
      display_name: 'Alex Renamed',
      company: {
        id: 'c2',
        owner_id: 'owner-1',
        domain: 'acme.com',
        name: 'Acme Co',
        created_at: '2026-07-01T00:00:00Z',
        updated_at: '2026-07-02T00:00:00Z',
      },
    }))

    const user = userEvent.setup()
    render(<PersonDetailScreen />)

    expect(await screen.findByText('Alex Doe')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /edit/i }))

    await user.clear(screen.getByLabelText('Display name'))
    await user.type(screen.getByLabelText('Display name'), 'Alex Renamed')
    await user.selectOptions(screen.getByLabelText('Company'), 'c2')
    await user.click(screen.getByRole('button', { name: /save changes/i }))

    expect(updatePersonMock).toHaveBeenCalledWith('p1', { displayName: 'Alex Renamed', companyId: 'c2' })
    expect(await screen.findByText('Alex Renamed')).toBeInTheDocument()
    expect(screen.getByText('Acme Co')).toBeInTheDocument()
  })

  it('merges into the selected person and navigates to the survivor', async () => {
    getPersonMock.mockResolvedValue(person())
    getPersonNotesMock.mockResolvedValue([])
    listCompaniesMock.mockResolvedValue([company()])
    listPeopleMock.mockResolvedValue([
      person(),
      person({ id: 'p2', display_name: 'Merge Target', primary_email: 'merge@example.com', company: null }),
      person({ id: 'p3', display_name: 'Another Target', primary_email: 'other@example.com', company: null }),
    ])
    mergePeopleMock.mockResolvedValue(person({ id: 'p2', display_name: 'Merge Target', primary_email: 'merge@example.com', company: null }))

    const user = userEvent.setup()
    render(<PersonDetailScreen />)

    expect(await screen.findByText('Alex Doe')).toBeInTheDocument()
    await user.selectOptions(screen.getByLabelText('Merge into'), 'p3')
    await user.click(screen.getByRole('button', { name: /^merge$/i }))

    expect(mergePeopleMock).toHaveBeenCalledWith('p1', 'p3')
    expect(navigateMock).toHaveBeenCalledWith('/people/p2')
  })

  it('blocks delete when confirmation is rejected', async () => {
    getPersonMock.mockResolvedValue(person())
    getPersonNotesMock.mockResolvedValue([])
    listCompaniesMock.mockResolvedValue([company()])
    listPeopleMock.mockResolvedValue([person(), person({ id: 'p2', display_name: 'Other Person', primary_email: 'other@example.com', company: null })])
    vi.spyOn(window, 'confirm').mockReturnValue(false)

    const user = userEvent.setup()
    render(<PersonDetailScreen />)

    expect(await screen.findByText('Alex Doe')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /^delete$/i }))

    expect(deletePersonMock).not.toHaveBeenCalled()
    expect(navigateMock).not.toHaveBeenCalled()
  })

  it('deletes the person after confirmation and navigates back to people', async () => {
    getPersonMock.mockResolvedValue(person())
    getPersonNotesMock.mockResolvedValue([])
    listCompaniesMock.mockResolvedValue([company()])
    listPeopleMock.mockResolvedValue([person(), person({ id: 'p2', display_name: 'Other Person', primary_email: 'other@example.com', company: null })])
    deletePersonMock.mockResolvedValue(undefined)
    vi.spyOn(window, 'confirm').mockReturnValue(true)

    const user = userEvent.setup()
    render(<PersonDetailScreen />)

    expect(await screen.findByText('Alex Doe')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /^delete$/i }))

    expect(deletePersonMock).toHaveBeenCalledWith('p1')
    expect(navigateMock).toHaveBeenCalledWith('/people')
  })
})
