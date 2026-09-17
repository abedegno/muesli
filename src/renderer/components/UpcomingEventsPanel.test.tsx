// @vitest-environment jsdom
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useLocation } from 'react-router-dom'
import type { CalendarEvent, EventBrief } from '../../shared/types'

const { getCalendarEvents } = vi.hoisted(() => ({ getCalendarEvents: vi.fn() }))

vi.mock('@/api', () => ({ muesli: { getCalendarEvents } }))

import { UpcomingEventsPanel } from './UpcomingEventsPanel'

const originalTimezone = process.env.TZ

const event = (overrides: Partial<CalendarEvent>): CalendarEvent => ({
  id: 'event-1',
  title: 'Planning',
  starts_at: '2026-07-09T09:00:00.000Z',
  ends_at: '2026-07-09T09:30:00.000Z',
  description: '',
  location: '',
  conferencing_url: '',
  attendees: [],
  source_id: 'calendar-1',
  ...overrides,
})

const brief = (overrides: Partial<EventBrief>): EventBrief => ({
  id: 'brief-1',
  template_id: 'template-1',
  template_name: 'Pre-read',
  status: 'ready',
  sections: [],
  model: '',
  updated_at: '2026-07-09T08:00:00.000Z',
  ...overrides,
})

function renderPanel() {
  function LocationProbe() {
    const location = useLocation()
    return <output data-testid="location">{`${location.pathname}${location.hash}`}</output>
  }
  return render(
    <MemoryRouter initialEntries={['/']}>
      <UpcomingEventsPanel />
      <LocationProbe />
    </MemoryRouter>,
  )
}

beforeAll(() => {
  process.env.TZ = 'UTC'
})

afterAll(() => {
  if (originalTimezone === undefined) delete process.env.TZ
  else process.env.TZ = originalTimezone
})

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-07-09T08:00:00.000Z'))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe('UpcomingEventsPanel', () => {
  it('shows loading placeholders while events are pending', () => {
    getCalendarEvents.mockReturnValue(new Promise(() => {}))
    const { container } = renderPanel()

    expect(container.querySelectorAll('.animate-pulse')).toHaveLength(4)
    expect(screen.queryByText('No upcoming events')).not.toBeInTheDocument()
  })

  it('shows the empty state and opens calendar settings', async () => {
    getCalendarEvents.mockResolvedValue([])
    renderPanel()

    expect(await screen.findByText('No upcoming events')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Open calendar settings' }))
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/settings#calendar'))
  })

  it('also falls back to the empty state when loading fails', async () => {
    getCalendarEvents.mockRejectedValue(new Error('not configured'))
    renderPanel()

    expect(await screen.findByText('No upcoming events')).toBeInTheDocument()
  })

  it('renders events chronologically with deterministic local-time ranges', async () => {
    getCalendarEvents.mockResolvedValue([
      event({ id: 'later', title: 'Later meeting', starts_at: '2026-07-09T11:00:00.000Z', ends_at: '2026-07-09T12:00:00.000Z' }),
      event({ id: 'earlier', title: 'Earlier meeting' }),
    ])
    renderPanel()

    const items = await screen.findAllByRole('listitem')
    expect(items.map((item) => item.textContent)).toEqual([
      expect.stringContaining('Earlier meeting'),
      expect.stringContaining('Later meeting'),
    ])
    expect(screen.getByText('9:00 AM – 9:30 AM')).toBeInTheDocument()
    expect(getCalendarEvents).toHaveBeenCalledWith(
      '2026-07-09T08:00:00.000Z',
      '2026-07-16T08:00:00.000Z',
    )
  })

  it('distinguishes an all-day midnight range from a timed event', async () => {
    getCalendarEvents.mockResolvedValue([
      event({ id: 'all-day', title: 'Company holiday', starts_at: '2026-07-10T00:00:00.000Z', ends_at: '2026-07-11T00:00:00.000Z' }),
      event({ id: 'timed', title: 'Morning sync', starts_at: '2026-07-10T09:00:00.000Z', ends_at: '2026-07-10T09:30:00.000Z' }),
    ])
    renderPanel()

    expect(await screen.findByText('12:00 AM – 12:00 AM')).toBeInTheDocument()
    expect(screen.getByText('9:00 AM – 9:30 AM')).toBeInTheDocument()
    expect(screen.getByText('Tomorrow')).toBeInTheDocument()
  })
  it('renders no Brief control when an event has an absent briefs field (rolling-upgrade compatibility)', async () => {
    getCalendarEvents.mockResolvedValue([event({})])
    renderPanel()

    await screen.findByText('Planning')
    expect(screen.queryByRole('button', { name: /brief/i })).not.toBeInTheDocument()
  })

  it('renders no Brief control when briefs is an empty array', async () => {
    getCalendarEvents.mockResolvedValue([event({ briefs: [] })])
    renderPanel()

    await screen.findByText('Planning')
    expect(screen.queryByRole('button', { name: /brief/i })).not.toBeInTheDocument()
  })

  it('shows an accessible, keyboard-operable Brief toggle that expands multiple template panels', async () => {
    getCalendarEvents.mockResolvedValue([
      event({
        briefs: [
          brief({
            id: 'b1',
            template_name: 'Pre-read',
            status: 'ready',
            sections: [{ heading: 'Context', content_markdown: '- Bullet one\n- Bullet two' }],
          }),
          brief({ id: 'b2', template_name: 'Attendee brief', status: 'pending' }),
        ],
      }),
    ])
    renderPanel()

    const toggle = await screen.findByRole('button', { name: /brief/i })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    // Collapsed: no panel content visible yet.
    expect(screen.queryByRole('region', { name: 'Pre-read brief' })).not.toBeInTheDocument()

    await userEvent.click(toggle)

    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    const readyPanel = screen.getByRole('region', { name: 'Pre-read brief' })
    expect(readyPanel).toBeInTheDocument()
    expect(readyPanel.querySelectorAll('li')).toHaveLength(2)
    expect(screen.getByRole('region', { name: 'Attendee brief brief' })).toHaveTextContent('Generating')

    // Keyboard operable: Tab to it and activate with Enter/Space via userEvent.keyboard.
    await userEvent.click(toggle) // collapse again
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    toggle.focus()
    await userEvent.keyboard('{Enter}')
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
  })

  it("shows a generic message for a failed brief without exposing internal diagnostics", async () => {
    getCalendarEvents.mockResolvedValue([
      event({ briefs: [brief({ id: 'b1', template_name: 'Pre-read', status: 'failed', sections: [] })] }),
    ])
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: /brief/i }))
    const panel = screen.getByRole('region', { name: 'Pre-read brief' })
    expect(panel).toHaveTextContent('Could not be generated.')
  })

  it('sanitizes hostile Markdown in a ready brief section (no raw HTML injected)', async () => {
    getCalendarEvents.mockResolvedValue([
      event({
        briefs: [
          brief({
            id: 'b1',
            status: 'ready',
            sections: [{ heading: '<img src=x onerror=alert(1)>', content_markdown: '<script>alert(1)</script>' }],
          }),
        ],
      }),
    ])
    renderPanel()

    await userEvent.click(await screen.findByRole('button', { name: /brief/i }))
    const panel = screen.getByRole('region', { name: /brief$/i })
    expect(panel.querySelector('script')).toBeNull()
    expect(panel.querySelector('img')).toBeNull()
    expect(panel.textContent).toContain('<script>alert(1)</script>')
  })
})
