// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import { InsightsScreen } from './InsightsScreen'
import type { InsightsResponse } from '../../shared/types'

const { getInsightsMock } = vi.hoisted(() => ({
  getInsightsMock: vi.fn(),
}))

vi.mock('@/api', () => ({
  muesli: {
    getInsights: (...args: unknown[]) => getInsightsMock(...args),
  },
}))

afterEach(cleanup)

beforeEach(() => {
  getInsightsMock.mockReset()
})

const insights = (over: Partial<InsightsResponse> = {}): InsightsResponse => ({
  meetings_per_day: [
    { day: '2026-07-01T00:00:00Z', count: 2 },
    { day: '2026-07-02T00:00:00Z', count: 3 },
  ],
  total_hours: 5.5,
  hours_per_week: [
    { week_start: '2026-06-30T00:00:00Z', hours: 5.5 },
  ],
  top_people: [
    {
      id: 'p1',
      primary_email: 'alex@example.com',
      display_name: 'Alex Doe',
      first_seen_at: '2026-06-01T00:00:00Z',
      updated_at: '2026-07-02T00:00:00Z',
      count: 4,
    },
  ],
  top_companies: [
    {
      id: 'c1',
      owner_id: 'owner-1',
      domain: 'example.com',
      name: 'Example Inc',
      created_at: '2026-06-01T00:00:00Z',
      updated_at: '2026-07-02T00:00:00Z',
      count: 4,
    },
  ],
  top_folders: [
    {
      id: 'f1',
      name: 'Sales',
      created_at: '2026-06-01T00:00:00Z',
      count: 2,
    },
  ],
  ...over,
})

describe('InsightsScreen', () => {
  it('renders summary cards and the meetings chart', async () => {
    getInsightsMock.mockResolvedValue(insights())

    render(<InsightsScreen />)

    const meetingsHeading = await screen.findByText('Total meetings')
    const hoursHeading = await screen.findByText('Total hours')
    const peopleHeading = await screen.findByText('Distinct people')
    const meetingsCard = meetingsHeading.closest('div') as HTMLElement
    const hoursCard = hoursHeading.closest('div') as HTMLElement
    const peopleCard = peopleHeading.closest('div') as HTMLElement

    expect(await within(meetingsCard).findByText('5')).toBeInTheDocument()
    expect(within(hoursCard).getByText('5.5')).toBeInTheDocument()
    expect(within(peopleCard).getByText('1')).toBeInTheDocument()
    expect(screen.getByRole('img', { name: /meetings over time chart/i })).toBeInTheDocument()
    expect(screen.getByText('Alex Doe')).toBeInTheDocument()
    expect(screen.getByText('Example Inc')).toBeInTheDocument()
    expect(screen.getByText('Sales')).toBeInTheDocument()
  })

  it('renders the empty state when the API returns zeroed data', async () => {
    getInsightsMock.mockResolvedValue({
      meetings_per_day: [],
      total_hours: 0,
      hours_per_week: [],
      top_people: [],
      top_companies: [],
      top_folders: [],
    })

    render(<InsightsScreen />)

    expect(await screen.findByText('No insights yet')).toBeInTheDocument()
    expect(screen.getByText('As meetings and linked notes accumulate, summary cards and charts will appear here.')).toBeInTheDocument()
  })
})
