// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import { ComingUpScreen } from './ComingUpScreen'
import type { CalendarEvent } from '../../shared/types'

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
})

const ev = (over: Partial<CalendarEvent>): CalendarEvent => ({
  id: 'id', title: 'Event', starts_at: '', ends_at: '', description: '', location: '',
  conferencing_url: '', attendees: [], source_id: 'src', ...over,
})

describe('ComingUpScreen', () => {
  it('fetches events on mount and renders them grouped with title, time, attendee count, and a conferencing badge', async () => {
    const now = new Date()
    const starts = new Date(now.getTime() + 60 * 60 * 1000).toISOString()
    const ends = new Date(now.getTime() + 90 * 60 * 1000).toISOString()
    getCalendarEventsMock.mockResolvedValue([
      ev({
        id: 'e1', title: 'Design review', starts_at: starts, ends_at: ends,
        attendees: [{ email: 'a@x.com', name: 'A', response: 'yes' }, { email: 'b@x.com', name: 'B', response: 'yes' }],
        conferencing_url: 'https://meet.example/e1',
      }),
    ])
    render(<ComingUpScreen />)
    await waitFor(() => expect(getCalendarEventsMock).toHaveBeenCalledTimes(1))
    expect(await screen.findByText('Design review')).toBeTruthy()
    expect(screen.getByText('2')).toBeTruthy()
    expect(screen.getByTitle('Video call')).toBeTruthy()
  })

  it('shows a friendly empty state when there are no events at all', async () => {
    getCalendarEventsMock.mockResolvedValue([])
    render(<ComingUpScreen />)
    expect(await screen.findByText('No upcoming events')).toBeTruthy()
  })

  it('shows a friendly empty state (not a crash) when getCalendarEvents rejects', async () => {
    getCalendarEventsMock.mockRejectedValue(new Error('no calendar source configured'))
    render(<ComingUpScreen />)
    expect(await screen.findByText('No upcoming events')).toBeTruthy()
  })

  it('renders "No events" for a day with zero events when other days have events', async () => {
    const now = new Date()
    const starts = new Date(now.getTime() + 60 * 60 * 1000).toISOString()
    const ends = new Date(now.getTime() + 90 * 60 * 1000).toISOString()
    getCalendarEventsMock.mockResolvedValue([ev({ id: 'e1', title: 'Only event', starts_at: starts, ends_at: ends })])
    render(<ComingUpScreen />)
    await waitFor(() => expect(getCalendarEventsMock).toHaveBeenCalledTimes(1))
    expect(await screen.findByText('Only event')).toBeTruthy()
    const noEvents = await screen.findAllByText('No events')
    expect(noEvents.length).toBeGreaterThan(0)
  })
})
