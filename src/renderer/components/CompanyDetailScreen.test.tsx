// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { CompanyDetailScreen } from './CompanyDetailScreen'
import type { CompanyWithPeople, Person } from '../../shared/types'

const { getCompanyMock, navigateMock, paramsState } = vi.hoisted(() => ({
  getCompanyMock: vi.fn(),
  navigateMock: vi.fn(),
  paramsState: { id: 'c1' as string },
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
    getCompany: (...args: Parameters<typeof getCompanyMock>) => getCompanyMock(...args),
  },
}))

afterEach(cleanup)

beforeEach(() => {
  getCompanyMock.mockReset()
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

describe('CompanyDetailScreen', () => {
  it('renders the company details and people', async () => {
    getCompanyMock.mockResolvedValue(company())

    render(<CompanyDetailScreen />)

    expect(await screen.findByText('Example Inc')).toBeInTheDocument()
    expect(screen.getByText('example.com')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Alex Doe alex@example.com' })).toHaveAttribute('href', '/people/p1')

    await userEvent.click(screen.getByRole('button', { name: /back to people/i }))
    expect(navigateMock).toHaveBeenCalledWith('/people')
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
