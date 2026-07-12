// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { PeopleScreen } from './PeopleScreen'
import type { CompanyWithCount, PersonWithCompany } from '../../shared/types'

const { listPeopleMock, listCompaniesMock } = vi.hoisted(() => ({
  listPeopleMock: vi.fn(),
  listCompaniesMock: vi.fn(),
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
    expect(screen.getByText('alex@example.com')).toBeInTheDocument()
    expect(screen.getByText('Example Inc')).toBeInTheDocument()
  })

  it('switches to the Companies tab and renders domain, name, and people_count', async () => {
    listPeopleMock.mockResolvedValue([])
    listCompaniesMock.mockResolvedValue([company()])

    render(<PeopleScreen />)

    expect(await screen.findByText('No people yet')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Companies' }))

    expect(await screen.findByText('Example Inc')).toBeInTheDocument()
    expect(screen.getByText('example.com')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
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
})
