// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, cleanup, waitFor, screen, fireEvent } from '@testing-library/react'

// --- Mocks -----------------------------------------------------------------

const navigate = vi.fn()
vi.mock('react-router-dom', () => ({
  useNavigate: () => navigate,
  useOutletContext: () => ({ refresh: vi.fn() }),
}))

function makeNote(id: string) {
  return {
    id,
    title: 'Meeting — Jun 28, 3:00 PM',
    status: 'recording' as const,
    created_at: '2026-07-16T00:00:00.000Z',
    updated_at: '2026-07-16T00:00:00.000Z',
    partial_transcript: false,
  }
}

vi.mock('@/api', () => ({
  muesli: {
    createNote: vi.fn(() => Promise.resolve(makeNote('note-123'))),
  },
}))

import { NewMeetingScreen } from './NewMeetingScreen'
import { muesli } from '@/api'

afterEach(() => {
  cleanup()
  navigate.mockClear()
  vi.mocked(muesli.createNote).mockClear()
})

describe('NewMeetingScreen', () => {
  it('calls muesli.createNote with a title containing "Meeting"', async () => {
    render(<NewMeetingScreen />)
    await waitFor(() => expect(muesli.createNote).toHaveBeenCalledTimes(1))
    const [titleArg] = vi.mocked(muesli.createNote).mock.calls[0]
    expect(titleArg).toMatch(/Meeting/)
  })

  it('navigates to /notes/<id>?capture=1 after creation', async () => {
    render(<NewMeetingScreen />)
    await waitFor(() => expect(navigate).toHaveBeenCalledTimes(1))
    expect(navigate).toHaveBeenCalledWith('/notes/note-123?capture=1', { replace: true })
  })

  it('shows a retry option and does not navigate when createNote rejects', async () => {
    vi.mocked(muesli.createNote).mockRejectedValueOnce(new Error('server not ready'))
    render(<NewMeetingScreen />)
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('server not ready'))
    expect(navigate).not.toHaveBeenCalled()
  })

  it('retrying after a failure calls createNote again and navigates on success', async () => {
    vi.mocked(muesli.createNote)
      .mockRejectedValueOnce(new Error('server not ready'))
      .mockResolvedValueOnce(makeNote('note-456'))

    render(<NewMeetingScreen />)
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))

    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/notes/note-456?capture=1', { replace: true }))
    expect(muesli.createNote).toHaveBeenCalledTimes(2)
  })
})
