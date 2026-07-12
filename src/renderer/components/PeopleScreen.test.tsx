// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { PeopleScreen } from './PeopleScreen'
import type { CompanyWithCount, PersonWithCompany } from '../../shared/types'

const { listPeopleMock, listCompaniesMock, navigateMock } = vi.hoisted(() => ({
  listPeopleMock: vi.fn(),
  listCompaniesMock: vi.fn(),
  navigateMock: vi.fn(),
}))

vi.mock('react-router-dom', () => ({
  useNavigate: () => navigateMock,
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
    listPeople: () => listPeopleMock(),
    listCompanies: () => listCompaniesMock(),
  },
}))

afterEach(cleanup)

beforeEach(() => {
  listPeopleMock.mockReset()
  listCompaniesMock.mockReset()
  navigateMock.mockReset()
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

describe('PeopleScreen', () => {
  it('renders people with their name, email, and company', async () => {
    listPeopleMock.mockResolvedValue([person()])
    listCompaniesMock.mockResolvedValue([])

    render(<PeopleScreen />)

    expect(await screen.findByText('Alex Doe')).toBeInTheDocument()
    expect(within(screen.getByText('Alex Doe').closest('li') as HTMLElement).getByText('A')).toBeInTheDocument()
    expect(screen.getByText('alex@example.com')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Example Inc' })).toHaveAttribute('href', '/companies/c1')

    await userEvent.click(screen.getByRole('button', { name: /alex doe/i }))
    expect(navigateMock).toHaveBeenCalledWith('/people/p1')
  })

  it('switches to the Companies tab and renders domain, name, and people_count', async () => {
    listPeopleMock.mockResolvedValue([])
    listCompaniesMock.mockResolvedValue([company()])

    render(<PeopleScreen />)

    expect(await screen.findByText('No people yet')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Companies' }))

    expect(await screen.findByText('Example Inc')).toBeInTheDocument()
    expect(within(screen.getByText('Example Inc').closest('li') as HTMLElement).getByText('E')).toBeInTheDocument()
    expect(screen.getByText('example.com')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /example inc/i }))
    expect(navigateMock).toHaveBeenCalledWith('/companies/c1')
  })

  it('shows the empty state when there are no people yet', async () => {
    listPeopleMock.mockResolvedValue([])
    listCompaniesMock.mockResolvedValue([])

    render(<PeopleScreen />)

    expect(await screen.findByText('No people yet')).toBeInTheDocument()
  })

  it('shows a visible error when listPeople rejects', async () => {
    listPeopleMock.mockRejectedValue(new Error('people service unavailable'))
    listCompaniesMock.mockResolvedValue([])

    render(<PeopleScreen />)

    expect(await screen.findByText(/could not load people/i)).toBeInTheDocument()
    expect(screen.getByText('people service unavailable')).toBeInTheDocument()
    expect(screen.queryByText('No people yet')).not.toBeInTheDocument()
  })

  it('filters people and companies client-side and shows a no-matches empty state', async () => {
    listPeopleMock.mockResolvedValue([
      person({
        id: 'p1',
        display_name: 'Alex Doe',
        primary_email: 'alex@example.com',
        company: {
          id: 'c1',
          owner_id: 'owner-1',
          domain: 'example.com',
          name: 'Example Inc',
          created_at: '2026-07-01T00:00:00Z',
          updated_at: '2026-07-02T00:00:00Z',
        },
      }),
      person({
        id: 'p2',
        display_name: 'Brianna Miles',
        primary_email: 'brianna@other.com',
        company: {
          id: 'c2',
          owner_id: 'owner-1',
          domain: 'other.com',
          name: 'Other LLC',
          created_at: '2026-07-01T00:00:00Z',
          updated_at: '2026-07-02T00:00:00Z',
        },
      }),
      person({
        id: 'p3',
        display_name: 'Casey Zhang',
        primary_email: 'casey@sample.org',
        company: {
          id: 'c3',
          owner_id: 'owner-1',
          domain: 'sample.org',
          name: 'Sample Co',
          created_at: '2026-07-01T00:00:00Z',
          updated_at: '2026-07-02T00:00:00Z',
        },
      }),
    ])
    listCompaniesMock.mockResolvedValue([
      company({ id: 'c1', name: 'Example Inc', domain: 'example.com', people_count: 10 }),
      company({ id: 'c2', name: 'Other LLC', domain: 'other.com', people_count: 4 }),
      company({ id: 'c3', name: 'Sample Co', domain: 'sample.org', people_count: 2 }),
    ])

    const user = userEvent.setup()
    render(<PeopleScreen />)

    expect(await screen.findByText('Alex Doe')).toBeInTheDocument()
    expect(screen.getByText('Brianna Miles')).toBeInTheDocument()
    expect(screen.getByText('Casey Zhang')).toBeInTheDocument()

    const search = screen.getByRole('textbox', { name: /search people and companies/i })

    await user.type(search, 'alex')
    expect(screen.getByText('Alex Doe')).toBeInTheDocument()
    expect(screen.queryByText('Brianna Miles')).not.toBeInTheDocument()
    expect(screen.queryByText('Casey Zhang')).not.toBeInTheDocument()

    await user.clear(search)
    expect(screen.getByText('Alex Doe')).toBeInTheDocument()
    expect(screen.getByText('Brianna Miles')).toBeInTheDocument()
    expect(screen.getByText('Casey Zhang')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Companies' }))
    await user.type(search, 'other')
    expect(screen.getByText('Other LLC')).toBeInTheDocument()
    expect(screen.queryByText('Example Inc')).not.toBeInTheDocument()
    expect(screen.queryByText('Sample Co')).not.toBeInTheDocument()

    await user.clear(search)
    expect(screen.getByText('Example Inc')).toBeInTheDocument()
    expect(screen.getByText('Other LLC')).toBeInTheDocument()
    expect(screen.getByText('Sample Co')).toBeInTheDocument()

    await user.clear(search)
    await user.type(search, 'zzzz')

    expect(await screen.findByText('No matches')).toBeInTheDocument()
    expect(screen.getByText('Try a different search for company names or domains.')).toBeInTheDocument()
  })

  it('sorts people and companies by name by default and reorders on most recent', async () => {
    listPeopleMock.mockResolvedValue([
      person({
        id: 'p1',
        display_name: 'Zoe Zebra',
        primary_email: 'zoe@example.com',
        first_seen_at: '2026-07-04T00:00:00Z',
        company: {
          id: 'c1',
          owner_id: 'owner-1',
          domain: 'zebra.com',
          name: 'Zebra Co',
          created_at: '2026-07-01T00:00:00Z',
          updated_at: '2026-07-02T00:00:00Z',
        },
      }),
      person({
        id: 'p2',
        display_name: 'Alex Arbor',
        primary_email: 'alex@example.com',
        first_seen_at: '2026-07-02T00:00:00Z',
        company: {
          id: 'c2',
          owner_id: 'owner-1',
          domain: 'arbor.com',
          name: 'Arbor LLC',
          created_at: '2026-07-03T00:00:00Z',
          updated_at: '2026-07-04T00:00:00Z',
        },
      }),
    ])
    listCompaniesMock.mockResolvedValue([
      company({
        id: 'c1',
        name: 'Zebra Co',
        domain: 'zebra.com',
        created_at: '2026-07-01T00:00:00Z',
        updated_at: '2026-07-02T00:00:00Z',
      }),
      company({
        id: 'c2',
        name: 'Arbor LLC',
        domain: 'arbor.com',
        created_at: '2026-07-03T00:00:00Z',
        updated_at: '2026-07-04T00:00:00Z',
      }),
    ])

    const user = userEvent.setup()
    render(<PeopleScreen />)

    await screen.findByText('Alex Arbor')
    expect(screen.getByText('2 people · 2 companies')).toBeInTheDocument()
    const peopleList = screen.getByRole('list')
    expect(within(peopleList).getAllByRole('listitem')[0]).toHaveTextContent('Alex Arbor')
    expect(within(peopleList).getAllByRole('listitem')[1]).toHaveTextContent('Zoe Zebra')

    await user.selectOptions(screen.getByLabelText('Sort people and companies'), 'recent')
    expect(within(peopleList).getAllByRole('listitem')[0]).toHaveTextContent('Zoe Zebra')
    expect(within(peopleList).getAllByRole('listitem')[1]).toHaveTextContent('Alex Arbor')

    await user.click(screen.getByRole('button', { name: 'Companies' }))
    const companyList = screen.getByRole('list')
    expect(within(companyList).getAllByRole('listitem')[0]).toHaveTextContent('Arbor LLC')
    expect(within(companyList).getAllByRole('listitem')[1]).toHaveTextContent('Zebra Co')
  })
})
