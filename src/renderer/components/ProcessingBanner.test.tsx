// @vitest-environment jsdom
import { describe, it, expect, afterEach, vi, beforeEach } from 'vitest'
import { render, screen, cleanup, act } from '@testing-library/react'
import { ProcessingBanner } from './ProcessingBanner'
import userEvent from '@testing-library/user-event'

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('ProcessingBanner', () => {
  it('shows an alert when processing failed', () => {
    render(<ProcessingBanner status="failed" />)
    expect(screen.getByRole('alert')).toHaveTextContent('Processing failed.')
  })

  it('calls onRetry when Re-run button is clicked', async () => {
    const user = userEvent.setup()
    const onRetry = vi.fn()
    render(<ProcessingBanner status="failed" onRetry={onRetry} />)
    await user.click(screen.getByRole('button', { name: 'Re-run' }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('shows a polite status while transcribing', () => {
    render(<ProcessingBanner status="transcribing" />)
    expect(screen.getByRole('status')).toHaveTextContent('Transcribing…')
  })

  it('shows a polite status while summarizing', () => {
    render(<ProcessingBanner status="summarizing" />)
    expect(screen.getByRole('status')).toHaveTextContent('Summarizing…')
  })

  it('shows a polite status while uploaded/queued', () => {
    render(<ProcessingBanner status="uploaded" />)
    expect(screen.getByRole('status')).toHaveTextContent('Uploaded — queued…')
  })

  it('does not show a Process next button when onProcessNext is omitted', () => {
    render(<ProcessingBanner status="uploaded" />)
    expect(screen.queryByRole('button', { name: 'Process next' })).not.toBeInTheDocument()
  })

  it.each(['uploaded', 'transcribing', 'summarizing'] as const)(
    'calls onProcessNext when Process next button is clicked while status=%s (a job may still be queued/pending)',
    async (status) => {
      const user = userEvent.setup()
      const onProcessNext = vi.fn()
      render(<ProcessingBanner status={status} onProcessNext={onProcessNext} />)
      await user.click(screen.getByRole('button', { name: 'Process next' }))
      expect(onProcessNext).toHaveBeenCalledTimes(1)
    },
  )

  it.each(['ready', 'failed', 'recording'] as const)(
    'does not show a Process next button while status=%s, even if onProcessNext is provided',
    (status) => {
      render(<ProcessingBanner status={status} onProcessNext={vi.fn()} />)
      expect(screen.queryByRole('button', { name: 'Process next' })).not.toBeInTheDocument()
    },
  )

  it('renders nothing while recording', () => {
    const { container } = render(<ProcessingBanner status="recording" />)
    expect(container.firstChild).toBeNull()
  })

  it('renders nothing when the note is ready', () => {
    const { container } = render(<ProcessingBanner status="ready" />)
    expect(container.firstChild).toBeNull()
  })

  describe('elapsed timer', () => {
    beforeEach(() => {
      vi.useFakeTimers()
    })

    it('shows elapsed seconds after 5 s when statusEnteredAt is provided', () => {
      const enteredAt = new Date(Date.now()).toISOString()
      render(<ProcessingBanner status="transcribing" statusEnteredAt={enteredAt} />)

      // Initially 0 s
      expect(screen.getByRole('status')).toHaveTextContent('0 s')

      // Advance 5 seconds
      act(() => {
        vi.advanceTimersByTime(5000)
      })

      expect(screen.getByRole('status')).toHaveTextContent('5 s')
    })

    it('resets counter to 0 when statusEnteredAt changes (status transition)', () => {
      const firstEnteredAt = new Date(Date.now()).toISOString()
      const { rerender } = render(
        <ProcessingBanner status="transcribing" statusEnteredAt={firstEnteredAt} />,
      )

      // Advance 10 seconds into the first status
      act(() => {
        vi.advanceTimersByTime(10000)
      })
      expect(screen.getByRole('status')).toHaveTextContent('10 s')

      // Simulate a status transition — new updated_at timestamp
      act(() => {
        vi.advanceTimersByTime(1000) // 1 ms gap so Date.now() moves
      })
      const newEnteredAt = new Date(Date.now()).toISOString()
      rerender(<ProcessingBanner status="summarizing" statusEnteredAt={newEnteredAt} />)

      // Counter should be back near 0
      expect(screen.getByRole('status')).toHaveTextContent('0 s')
    })

    it('clears the interval on unmount (no pending timers left)', () => {
      const enteredAt = new Date(Date.now()).toISOString()
      const { unmount } = render(
        <ProcessingBanner status="transcribing" statusEnteredAt={enteredAt} />,
      )

      unmount()

      // Advancing time after unmount should not cause errors
      expect(() => {
        act(() => {
          vi.advanceTimersByTime(5000)
        })
      }).not.toThrow()

      // No pending fake timers remain from the banner
      expect(vi.getTimerCount()).toBe(0)
    })

    it('does not show elapsed counter when statusEnteredAt is omitted', () => {
      render(<ProcessingBanner status="transcribing" />)
      act(() => {
        vi.advanceTimersByTime(5000)
      })
      // Should show the label without " s" suffix
      const el = screen.getByRole('status')
      expect(el).toHaveTextContent('Transcribing…')
      expect(el.textContent).not.toMatch(/\d+ s/)
    })
  })

  describe('model download progress (TR03)', () => {
    // Tests that only need the initial async poll resolved use real timers
    // (findByTestId / waitFor rely on setTimeout for retries; fake timers block them).

    it('shows download progress banner when plugin is downloading during transcription', async () => {
      const getDownloadStatus = vi.fn().mockResolvedValue({
        status: 'downloading',
        model: 'base',
        percent: 42,
      })

      render(
        <ProcessingBanner
          status="transcribing"
          onGetDownloadStatus={getDownloadStatus}
        />,
      )

      // findByTestId retries with real timers until the async state update lands.
      const banner = await screen.findByTestId('download-progress-banner')
      expect(banner).toHaveTextContent('Downloading model base (42%)')
    })

    it('does not show download banner when plugin status is not downloading', async () => {
      const getDownloadStatus = vi.fn().mockResolvedValue({
        status: 'ready',
        model: 'base',
        percent: 100,
      })

      render(
        <ProcessingBanner
          status="transcribing"
          onGetDownloadStatus={getDownloadStatus}
        />,
      )

      await act(async () => { await Promise.resolve() })

      expect(screen.queryByTestId('download-progress-banner')).not.toBeInTheDocument()
    })

    it('does not show download banner when onGetDownloadStatus is not provided', () => {
      render(<ProcessingBanner status="transcribing" />)
      expect(screen.queryByTestId('download-progress-banner')).not.toBeInTheDocument()
    })

    // The following polling-interval tests require fake timers.
    describe('with fake timers', () => {
      beforeEach(() => {
        vi.useFakeTimers()
      })

      it('polls every 3 seconds while transcribing', async () => {
        const getDownloadStatus = vi.fn().mockResolvedValue({
          status: 'downloading',
          model: 'base',
          percent: 10,
        })

        render(
          <ProcessingBanner
            status="transcribing"
            onGetDownloadStatus={getDownloadStatus}
          />,
        )

        // Initial poll fires immediately.
        await act(async () => { await Promise.resolve() })
        expect(getDownloadStatus).toHaveBeenCalledTimes(1)

        // Advance 3 seconds — second poll.
        await act(async () => {
          vi.advanceTimersByTime(3000)
          await Promise.resolve()
        })
        expect(getDownloadStatus).toHaveBeenCalledTimes(2)

        // Advance another 3 seconds — third poll.
        await act(async () => {
          vi.advanceTimersByTime(3000)
          await Promise.resolve()
        })
        expect(getDownloadStatus).toHaveBeenCalledTimes(3)
      })

      it('clears download status and stops polling when status changes away from transcribing', async () => {
        const getDownloadStatus = vi.fn().mockResolvedValue({
          status: 'downloading',
          model: 'base',
          percent: 20,
        })

        const { rerender } = render(
          <ProcessingBanner
            status="transcribing"
            onGetDownloadStatus={getDownloadStatus}
          />,
        )

        await act(async () => { await Promise.resolve() })
        expect(getDownloadStatus).toHaveBeenCalledTimes(1)

        // Transition to summarizing — polling should stop.
        rerender(
          <ProcessingBanner
            status="summarizing"
            onGetDownloadStatus={getDownloadStatus}
          />,
        )

        await act(async () => {
          vi.advanceTimersByTime(6000)
          await Promise.resolve()
        })
        // Should not have been called again after status change.
        expect(getDownloadStatus).toHaveBeenCalledTimes(1)
        expect(screen.queryByTestId('download-progress-banner')).not.toBeInTheDocument()
      })
    })
  })
})
