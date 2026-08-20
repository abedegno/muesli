// @vitest-environment jsdom
import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { InsightsScreen } from './InsightsScreen'
import type { InsightsResponse } from '../../shared/types'
import { clearAgentCapabilityCache } from '@/lib/agentCapability'

const { getInsightsMock, getCapabilitiesMock } = vi.hoisted(() => ({
  getInsightsMock: vi.fn(),
  getCapabilitiesMock: vi.fn(),
}))

vi.mock('@/api', () => ({
  muesli: {
    getInsights: (...args: unknown[]) => getInsightsMock(...args),
    getCapabilities: () => getCapabilitiesMock(),
  },
}))

afterEach(() => {
  cleanup()
  getInsightsMock.mockReset()
  getCapabilitiesMock.mockReset()
  clearAgentCapabilityCache()
})

beforeEach(() => {
  getCapabilitiesMock.mockResolvedValue({ agentConfigured: true })
})

function utcDateString(date: Date): string {
  return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate())).toISOString().slice(0, 10)
}

function daysAgo(days: number, now = new Date()): string {
  const date = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()))
  date.setUTCDate(date.getUTCDate() - days)
  return utcDateString(date)
}

const today = utcDateString(new Date())
const thirtyDaysAgo = daysAgo(30)
const sevenDaysAgo = daysAgo(7)
const customFrom = '2026-06-01'
const customTo = '2026-06-20'

function insights(over: Partial<InsightsResponse> = {}): InsightsResponse {
  return {
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
  }
}

describe('InsightsScreen', () => {
  it('shows a non-blocking agent notice without replacing calendar-derived insights', async () => {
    getCapabilitiesMock.mockResolvedValue({ agentConfigured: false })
    getInsightsMock.mockResolvedValue(insights())
    render(<InsightsScreen />)
    expect(await screen.findByText(/AI features need an agent plugin configured/i)).toBeInTheDocument()
    expect(await screen.findByText('Total meetings')).toBeInTheDocument()
  })

  it('reloads insights when a preset changes', async () => {
    getCapabilitiesMock.mockResolvedValue({ agentConfigured: true })
    getInsightsMock.mockImplementation((from?: string, to?: string) => {
      if (from === thirtyDaysAgo && to === today) {
        return Promise.resolve(insights())
      }

      if (from === sevenDaysAgo && to === today) {
        return Promise.resolve(
          insights({
            meetings_per_day: [
              { day: '2026-07-07T00:00:00Z', count: 4 },
              { day: '2026-07-08T00:00:00Z', count: 5 },
            ],
            total_hours: 8.25,
            hours_per_week: [
              { week_start: '2026-07-07T00:00:00Z', hours: 8.25 },
            ],
            top_people: [
              {
                id: 'p2',
                primary_email: 'jordan@example.com',
                display_name: 'Jordan Lee',
                first_seen_at: '2026-07-07T00:00:00Z',
                updated_at: '2026-07-08T00:00:00Z',
                count: 6,
              },
              {
                id: 'p3',
                primary_email: 'sam@example.com',
                display_name: 'Sam Park',
                first_seen_at: '2026-07-07T00:00:00Z',
                updated_at: '2026-07-08T00:00:00Z',
                count: 2,
              },
            ],
            top_companies: [
              {
                id: 'c2',
                owner_id: 'owner-2',
                domain: 'acme.test',
                name: 'Acme',
                created_at: '2026-07-07T00:00:00Z',
                updated_at: '2026-07-08T00:00:00Z',
                count: 5,
              },
            ],
            top_folders: [
              {
                id: 'f2',
                name: 'Customer Calls',
                created_at: '2026-07-07T00:00:00Z',
                count: 3,
              },
            ],
          }),
        )
      }

      throw new Error(`unexpected insight request: ${from ?? 'undefined'} -> ${to ?? 'undefined'}`)
    })

    const user = userEvent.setup({ delay: null })
    render(<InsightsScreen />)

    const meetingsHeading = await screen.findByText('Total meetings')
    const hoursHeading = await screen.findByText('Total hours')
    const peopleHeading = await screen.findByText('Distinct people')
    const meetingsCard = meetingsHeading.closest('div') as HTMLElement
    const hoursCard = hoursHeading.closest('div') as HTMLElement
    const peopleCard = peopleHeading.closest('div') as HTMLElement

    expect(getInsightsMock).toHaveBeenCalledWith(thirtyDaysAgo, today)
    expect(within(meetingsCard).getByText('5')).toBeInTheDocument()
    expect(within(hoursCard).getByText('5.5')).toBeInTheDocument()
    expect(within(peopleCard).getByText('1')).toBeInTheDocument()
    expect(screen.getByText('Jul 1: 2 meetings', { selector: 'li' })).toBeInTheDocument()
    expect(screen.getByText('Alex Doe')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '7d' }))

    await screen.findByText('Jul 7: 4 meetings', { selector: 'li' })
    expect(getInsightsMock).toHaveBeenLastCalledWith(sevenDaysAgo, today)
    expect(within(meetingsCard).getByText('9')).toBeInTheDocument()
    expect(within(hoursCard).getByText('8.3')).toBeInTheDocument()
    expect(within(peopleCard).getByText('2')).toBeInTheDocument()
    expect(screen.getByText('Jordan Lee')).toBeInTheDocument()
    expect(screen.getByText('Acme')).toBeInTheDocument()
    expect(screen.getByText('Customer Calls')).toBeInTheDocument()
  })

  it('reloads insights when a custom range is applied', async () => {
    getInsightsMock.mockImplementation((from?: string, to?: string) => {
      if (from === thirtyDaysAgo && to === today) {
        return Promise.resolve(insights())
      }

      if (from === customFrom && to === customTo) {
        return Promise.resolve(
          insights({
            meetings_per_day: [
              { day: '2026-06-01T00:00:00Z', count: 1 },
              { day: '2026-06-20T00:00:00Z', count: 2 },
            ],
            total_hours: 3,
            hours_per_week: [
              { week_start: '2026-06-01T00:00:00Z', hours: 3 },
            ],
            top_people: [
              {
                id: 'p4',
                primary_email: 'morgan@example.com',
                display_name: 'Morgan Roe',
                first_seen_at: '2026-06-01T00:00:00Z',
                updated_at: '2026-06-20T00:00:00Z',
                count: 3,
              },
            ],
            top_companies: [],
            top_folders: [],
          }),
        )
      }

      throw new Error(`unexpected insight request: ${from ?? 'undefined'} -> ${to ?? 'undefined'}`)
    })

    const user = userEvent.setup({ delay: null })
    render(<InsightsScreen />)

    const meetingsHeading = await screen.findByText('Total meetings')
    const hoursHeading = await screen.findByText('Total hours')
    const peopleHeading = await screen.findByText('Distinct people')
    const meetingsCard = meetingsHeading.closest('div') as HTMLElement
    const hoursCard = hoursHeading.closest('div') as HTMLElement
    const peopleCard = peopleHeading.closest('div') as HTMLElement
    expect(getInsightsMock).toHaveBeenCalledWith(thirtyDaysAgo, today)

    await user.click(screen.getByRole('button', { name: 'custom' }))
    fireEvent.change(screen.getByLabelText('Custom range start date'), { target: { value: customFrom } })
    fireEvent.change(screen.getByLabelText('Custom range end date'), { target: { value: customTo } })
    await user.click(screen.getByRole('button', { name: /apply custom range/i }))

    await screen.findByText('Jun 1: 1 meetings', { selector: 'li' })
    expect(getInsightsMock).toHaveBeenLastCalledWith(customFrom, customTo)
    expect(within(meetingsCard).getByText('3')).toBeInTheDocument()
    expect(within(hoursCard).getByText('3')).toBeInTheDocument()
    expect(within(peopleCard).getByText('1')).toBeInTheDocument()
    expect(screen.getByText('Morgan Roe')).toBeInTheDocument()
  })

  it('fills chart bars with the primary token directly, not wrapped in hsl()', () => {
    const source = readFileSync('src/renderer/components/InsightsScreen.tsx', 'utf8')
    expect(source).not.toContain('hsl(var(--primary))')
    expect(source).toContain('fill="var(--primary)"')
  })
})
