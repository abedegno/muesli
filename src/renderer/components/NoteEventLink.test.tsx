// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { NoteEventLink } from './NoteEventLink'
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

describe('NoteEventLink', () => {
  it('shows the picker with upcoming events once opened, and calls onLink when one is picked', async () => {
    const user = userEvent.setup()
    const now = new Date()
    const starts = new Date(now.getTime() + 60 * 60 * 1000).toISOString()
    const ends = new Date(now.getTime() + 90 * 60 * 1000).toISOString()
    getCalendarEventsMock.mockResolvedValue([ev({ id: 'e1', title: 'Design review', starts_at: starts, ends_at: ends })])
    const onLink = vi.fn()
    render(<NoteEventLink onLink={onLink} onUnlink={vi.fn()} />)

    expect(screen.queryByText('Design review')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /link to calendar event/i }))
    expect(await screen.findByText('Design review')).toBeInTheDocument()

    await user.click(screen.getByText('Design review'))
    expect(onLink).toHaveBeenCalledWith('e1')
  })

  it('shows a graceful empty state when there are no upcoming events to pick from', async () => {
    const user = userEvent.setup()
    getCalendarEventsMock.mockResolvedValue([])
    render(<NoteEventLink onLink={vi.fn()} onUnlink={vi.fn()} />)
    await user.click(screen.getByRole('button', { name: /link to calendar event/i }))
    expect(await screen.findByText('No upcoming events')).toBeInTheDocument()
  })

  it('shows a graceful empty state (not a crash) when getCalendarEvents rejects', async () => {
    const user = userEvent.setup()
    getCalendarEventsMock.mockRejectedValue(new Error('no calendar source configured'))
    render(<NoteEventLink onLink={vi.fn()} onUnlink={vi.fn()} />)
    await user.click(screen.getByRole('button', { name: /link to calendar event/i }))
    expect(await screen.findByText('No upcoming events')).toBeInTheDocument()
  })

  it('renders the linked event title + time when eventId is set', async () => {
    const starts = new Date(2026, 5, 13, 14, 0).toISOString()
    const ends = new Date(2026, 5, 13, 14, 30).toISOString()
    getCalendarEventsMock.mockResolvedValue([ev({ id: 'e1', title: 'Design review', starts_at: starts, ends_at: ends })])
    render(<NoteEventLink eventId="e1" onLink={vi.fn()} onUnlink={vi.fn()} />)
    expect(await screen.findByText('Design review')).toBeInTheDocument()
    expect(screen.getByText(/2:00.*2:30/)).toBeInTheDocument()
  })

  it('resolves the linked event title + time even when it is far outside a narrow (e.g. 90-day) window', async () => {
    // The note's meeting happened ~200 days ago - well past a naive +/-90-day
    // lookup window, but still within the wider window NoteEventLink actually
    // uses (LINKED_EVENT_LOOKUP_DAYS = 730), so the real title/time must render.
    const now = new Date()
    const starts = new Date(now.getTime() - 200 * 24 * 60 * 60 * 1000)
    const ends = new Date(starts.getTime() + 30 * 60 * 1000)
    getCalendarEventsMock.mockResolvedValue([
      ev({ id: 'e1', title: 'Quarterly planning', starts_at: starts.toISOString(), ends_at: ends.toISOString() }),
    ])
    render(<NoteEventLink eventId="e1" onLink={vi.fn()} onUnlink={vi.fn()} />)
    expect(await screen.findByText('Quarterly planning')).toBeInTheDocument()
    // getCalendarEvents was called with a range wide enough to include an event 200 days back.
    const [from, to] = getCalendarEventsMock.mock.calls[0]
    expect(new Date(from as string).getTime()).toBeLessThanOrEqual(starts.getTime())
    expect(new Date(to as string).getTime()).toBeGreaterThanOrEqual(now.getTime())
  })

  it('renders a distinct, honest fallback (no crash, and not a fabricated title) when the linked event id cannot be resolved within the lookup window', async () => {
    getCalendarEventsMock.mockResolvedValue([])
    render(<NoteEventLink eventId="missing" onLink={vi.fn()} onUnlink={vi.fn()} />)
    await waitFor(() => expect(getCalendarEventsMock).toHaveBeenCalled())
    expect(await screen.findByText('Linked event (details unavailable)')).toBeInTheDocument()
  })

  it('calls onUnlink when Unlink is clicked on a linked note', async () => {
    const user = userEvent.setup()
    getCalendarEventsMock.mockResolvedValue([ev({ id: 'e1', title: 'Design review' })])
    const onUnlink = vi.fn()
    render(<NoteEventLink eventId="e1" onLink={vi.fn()} onUnlink={onUnlink} />)
    await screen.findByText('Design review')
    await user.click(screen.getByRole('button', { name: /unlink calendar event/i }))
    expect(onUnlink).toHaveBeenCalledTimes(1)
  })
})
