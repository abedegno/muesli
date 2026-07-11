// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, cleanup, waitFor } from '@testing-library/react'

// --- Mocks -----------------------------------------------------------------

const navigate = vi.fn()
vi.mock('react-router-dom', () => ({
  useNavigate: () => navigate,
  useOutletContext: () => ({ refresh: vi.fn() }),
}))

vi.mock('@/api', () => ({
  muesli: {
    createNote: vi.fn(() =>
      Promise.resolve({ id: 'note-123', title: 'Meeting — Jun 28, 3:00 PM' }),
    ),
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
})
