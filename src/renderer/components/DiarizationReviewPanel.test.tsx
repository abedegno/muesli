// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { DiarizationReview } from '../../shared/types'

const { getDiarizationReviewMock, postDiarizationReviewMock } = vi.hoisted(() => ({
  getDiarizationReviewMock: vi.fn(),
  postDiarizationReviewMock: vi.fn(),
}))

vi.mock('@/api', () => ({
  muesli: {
    getDiarizationReview: getDiarizationReviewMock,
    postDiarizationReview: postDiarizationReviewMock,
  },
}))

import { DiarizationReviewPanel } from './DiarizationReviewPanel'

// generation 5 is an arbitrary, distinctive fixture value — distinct from the
// zero the server treats as "absent" — so an assertion that checks it is
// actually checking the panel echoed back the value it was rendered from,
// not merely that some truthy number was sent.
function makeReview(reviewState = 'pending', generation = 5): DiarizationReview {
  return {
    note_id: 'n1',
    review_state: reviewState,
    turns: [
      { id: 't1', start_ms: 0, end_ms: 1000, text: 'turn one text', source: 'mixed', speaker: 'SPEAKER_00', confidence: 0.2 },
      { id: 't2', start_ms: 1000, end_ms: 2000, text: 'turn two text', source: 'mixed', speaker: 'SPEAKER_01', confidence: 0.4 },
      { id: 't3', start_ms: 2000, end_ms: 3000, text: 'turn three text', source: 'mixed', speaker: 'SPEAKER_00', confidence: 0.6 },
    ],
    generation,
  }
}

// The shape a real 409 arrives in: BridgeError carries `status`, recovered from
// the `[409] ` prefix ipcHandlers encodes into the message. That wire path —
// server 409 -> ApiError -> `[409] ` message -> BridgeError.status — is bound
// by src/main/ipcHandlers.test.ts and src/renderer/api.test.ts; this file binds
// what the panel does once it has the status.
function conflictError(): Error {
  return Object.assign(new Error('transcript changed, refetch and retry'), { status: 409 })
}

beforeEach(() => {
  getDiarizationReviewMock.mockReset().mockResolvedValue(makeReview())
  postDiarizationReviewMock.mockReset().mockImplementation(async (_noteId: string, body: { reviewState?: string; generation: number }) =>
    makeReview(body.reviewState ?? 'in_review', body.generation))
})

afterEach(cleanup)

async function openPanel() {
  const btn = await screen.findByRole('button', { name: /review speakers/i })
  await userEvent.click(btn)
  await screen.findByRole('dialog', { name: /diarization review/i })
}

describe('DiarizationReviewPanel', () => {
  it('is hidden when there is no transcript', () => {
    render(<DiarizationReviewPanel noteId="n1" hasTranscript={false} />)
    expect(screen.queryByRole('button', { name: /review speakers/i })).not.toBeInTheDocument()
    expect(getDiarizationReviewMock).not.toHaveBeenCalled()
  })

  it('is hidden when the review fetch fails', async () => {
    getDiarizationReviewMock.mockRejectedValue(new Error('boom'))
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await waitFor(() => expect(getDiarizationReviewMock).toHaveBeenCalled())
    expect(screen.queryByRole('button', { name: /review speakers/i })).not.toBeInTheDocument()
  })

  it('is hidden when review_state is already completed', async () => {
    getDiarizationReviewMock.mockResolvedValue(makeReview('completed'))
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await waitFor(() => expect(getDiarizationReviewMock).toHaveBeenCalled())
    expect(screen.queryByRole('button', { name: /review speakers/i })).not.toBeInTheDocument()
  })

  it('shows the entry point and lists turns in server order (lowest confidence first)', async () => {
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(3)
    expect(options[0]).toHaveTextContent('turn one text')
    expect(options[1]).toHaveTextContent('turn two text')
    expect(options[2]).toHaveTextContent('turn three text')
  })

  it('transitions review_state pending -> in_review (best-effort) on open', async () => {
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    await waitFor(() =>
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { reviewState: 'in_review', generation: 5 }),
    )
  })

  it('the first turn receives focus as the single Tab stop (roving tabindex)', async () => {
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    const options = screen.getAllByRole('option')
    await waitFor(() => expect(options[0]).toHaveFocus())
    expect(options[0]).toHaveAttribute('tabindex', '0')
    expect(options[1]).toHaveAttribute('tabindex', '-1')
    expect(options[2]).toHaveAttribute('tabindex', '-1')
  })

  it('ArrowDown/ArrowUp move focus between turns, keeping a single tab stop', async () => {
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    const options = screen.getAllByRole('option')
    await waitFor(() => expect(options[0]).toHaveFocus())

    fireEvent.keyDown(options[0], { key: 'ArrowDown' })
    await waitFor(() => expect(options[1]).toHaveFocus())
    expect(options[1]).toHaveAttribute('tabindex', '0')
    expect(options[0]).toHaveAttribute('tabindex', '-1')

    fireEvent.keyDown(options[1], { key: 'ArrowUp' })
    await waitFor(() => expect(options[0]).toHaveFocus())
  })

  it('Enter confirms the focused turn\'s current speaker via POST', async () => {
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    const options = screen.getAllByRole('option')
    await waitFor(() => expect(options[0]).toHaveFocus())

    fireEvent.keyDown(options[0], { key: 'Enter' })
    await waitFor(() =>
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't1', speaker: 'SPEAKER_00', generation: 5 }),
    )
    expect(await within(options[0]).findByText('Confirmed')).toBeInTheDocument()
  })

  it('reassigns a turn to an alternative speaker before confirming', async () => {
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    const options = screen.getAllByRole('option')

    // turn two currently shows SPEAKER_01 — reassign it to SPEAKER_00 via the
    // explicit per-alternative control, then confirm with Enter.
    const reassignBtn = within(options[1]).getByRole('button', { name: /reassign turn 2 to speaker_00/i })
    await userEvent.click(reassignBtn)
    expect(within(options[1]).getByTestId('current-speaker')).toHaveTextContent('SPEAKER_00')

    fireEvent.keyDown(options[1], { key: 'Enter' })
    await waitFor(() =>
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't2', speaker: 'SPEAKER_00', generation: 5 }),
    )
  })

  it('ArrowLeft/ArrowRight cycle the focused turn\'s speaker among alternatives', async () => {
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    const options = screen.getAllByRole('option')
    await waitFor(() => expect(options[0]).toHaveFocus())

    // turn one starts at SPEAKER_00; cycling right should move to SPEAKER_01.
    fireEvent.keyDown(options[0], { key: 'ArrowRight' })
    await waitFor(() => expect(within(options[0]).getByTestId('current-speaker')).toHaveTextContent('SPEAKER_01'))

    fireEvent.keyDown(options[0], { key: 'Enter' })
    await waitFor(() =>
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't1', speaker: 'SPEAKER_01', generation: 5 }),
    )
  })

  it('Accept all remaining confirms every not-yet-confirmed turn, skipping already-confirmed ones', async () => {
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    const options = screen.getAllByRole('option')

    // Confirm the first turn individually first.
    fireEvent.keyDown(options[0], { key: 'Enter' })
    await waitFor(() =>
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't1', speaker: 'SPEAKER_00', generation: 5 }),
    )
    postDiarizationReviewMock.mockClear()

    await userEvent.click(screen.getByRole('button', { name: /accept all remaining/i }))

    await waitFor(() => {
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't2', speaker: 'SPEAKER_01', generation: 5 })
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't3', speaker: 'SPEAKER_00', generation: 5 })
    })
    // The already-confirmed turn must NOT be re-posted by the bulk action.
    expect(postDiarizationReviewMock).not.toHaveBeenCalledWith('n1', { segmentId: 't1', speaker: 'SPEAKER_00', generation: 5 })
    expect(postDiarizationReviewMock).toHaveBeenCalledTimes(2)
  })

  it('marking the review complete posts review_state=completed and hides the entry point', async () => {
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()

    await userEvent.click(screen.getByRole('button', { name: /mark review complete/i }))

    await waitFor(() =>
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { reviewState: 'completed', generation: 5 }),
    )
    await waitFor(() => expect(screen.queryByRole('button', { name: /review speakers/i })).not.toBeInTheDocument())
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
  it('treats a 409 on confirm as a replaced transcript: refetches, says so, and does not resubmit', async () => {
    postDiarizationReviewMock.mockImplementation(async (_noteId: string, body: { segmentId?: string; reviewState?: string; generation: number }) => {
      if (body.segmentId) throw conflictError()
      return makeReview(body.reviewState ?? 'in_review', body.generation)
    })
    // The replacement the refetch finds, at a different generation.
    getDiarizationReviewMock.mockResolvedValueOnce(makeReview()).mockResolvedValue(makeReview('in_review', 9))

    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    const options = screen.getAllByRole('option')
    await waitFor(() => expect(options[0]).toHaveFocus())

    fireEvent.keyDown(options[0], { key: 'Enter' })

    // The user is told, in the panel, that their edit was not saved.
    const notice = await screen.findByTestId('review-conflict')
    expect(notice).toHaveTextContent(/was not saved/i)
    // The review was refetched (once on mount, once after the conflict).
    await waitFor(() => expect(getDiarizationReviewMock).toHaveBeenCalledTimes(2))
    // The turn is NOT shown as confirmed, and the edit was not resubmitted.
    expect(within(screen.getAllByRole('option')[0]).queryByText('Confirmed')).not.toBeInTheDocument()
    const segmentPosts = postDiarizationReviewMock.mock.calls.filter((c) => c[1].segmentId)
    expect(segmentPosts).toHaveLength(1)
  })

  it('after a conflict, later actions carry the refetched generation rather than the dead one', async () => {
    let failNextSegmentPost = true
    postDiarizationReviewMock.mockImplementation(async (_noteId: string, body: { segmentId?: string; reviewState?: string; generation: number }) => {
      if (body.segmentId && failNextSegmentPost) {
        failNextSegmentPost = false
        throw conflictError()
      }
      return makeReview(body.reviewState ?? 'in_review', body.generation)
    })
    getDiarizationReviewMock.mockResolvedValueOnce(makeReview()).mockResolvedValue(makeReview('in_review', 9))

    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    fireEvent.keyDown(screen.getAllByRole('option')[0], { key: 'Enter' })
    await screen.findByTestId('review-conflict')

    // A fresh, deliberate action by the user — never an automatic retry.
    fireEvent.keyDown(screen.getAllByRole('option')[0], { key: 'Enter' })
    await waitFor(() =>
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't1', speaker: 'SPEAKER_00', generation: 9 }),
    )
  })

  it('stops Accept all remaining at the first 409 instead of reporting "Confirmed 0 of 3"', async () => {
    postDiarizationReviewMock.mockImplementation(async (_noteId: string, body: { segmentId?: string; reviewState?: string; generation: number }) => {
      if (body.segmentId) throw conflictError()
      return makeReview(body.reviewState ?? 'in_review', body.generation)
    })
    getDiarizationReviewMock.mockResolvedValueOnce(makeReview()).mockResolvedValue(makeReview('in_review', 9))

    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    await userEvent.click(screen.getByRole('button', { name: /accept all remaining/i }))

    await screen.findByTestId('review-conflict')
    // Every remaining turn carries the same dead generation, so continuing the
    // batch can only produce more 409s.
    const segmentPosts = postDiarizationReviewMock.mock.calls.filter((c) => c[1].segmentId)
    expect(segmentPosts).toHaveLength(1)
    await waitFor(() => expect(getDiarizationReviewMock).toHaveBeenCalledTimes(2))
  })

  it('surfaces a 409 on Mark review complete without hiding the panel', async () => {
    postDiarizationReviewMock.mockImplementation(async (_noteId: string, body: { segmentId?: string; reviewState?: string; generation: number }) => {
      if (body.reviewState === 'completed') throw conflictError()
      return makeReview(body.reviewState ?? 'in_review', body.generation)
    })
    getDiarizationReviewMock.mockResolvedValueOnce(makeReview()).mockResolvedValue(makeReview('in_review', 9))

    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    await userEvent.click(screen.getByRole('button', { name: /mark review complete/i }))

    await screen.findByTestId('review-conflict')
    // The review was NOT completed, so the entry point must not disappear.
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await waitFor(() => expect(getDiarizationReviewMock).toHaveBeenCalledTimes(2))
  })

  it('refetches when the open-panel transition itself conflicts, so later actions are not stale', async () => {
    postDiarizationReviewMock.mockImplementation(async (_noteId: string, body: { segmentId?: string; reviewState?: string; generation: number }) => {
      if (body.reviewState === 'in_review') throw conflictError()
      return makeReview(body.reviewState ?? 'in_review', body.generation)
    })
    getDiarizationReviewMock.mockResolvedValueOnce(makeReview()).mockResolvedValue(makeReview('in_review', 9))

    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()

    await screen.findByTestId('review-conflict')
    await waitFor(() => expect(getDiarizationReviewMock).toHaveBeenCalledTimes(2))

    // The panel is now bound to the refetched transcript, not the dead one.
    fireEvent.keyDown(screen.getAllByRole('option')[0], { key: 'Enter' })
    await waitFor(() =>
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't1', speaker: 'SPEAKER_00', generation: 9 }),
    )
  })

  it('does not treat an ordinary failure as a conflict', async () => {
    postDiarizationReviewMock.mockImplementation(async (_noteId: string, body: { segmentId?: string; reviewState?: string; generation: number }) => {
      if (body.segmentId) throw new Error('socket hang up')
      return makeReview(body.reviewState ?? 'in_review', body.generation)
    })

    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    fireEvent.keyDown(screen.getAllByRole('option')[0], { key: 'Enter' })

    await waitFor(() => expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't1', speaker: 'SPEAKER_00', generation: 5 }))
    // No conflict notice, and no refetch: the user's in-progress review stands.
    expect(screen.queryByTestId('review-conflict')).not.toBeInTheDocument()
    expect(getDiarizationReviewMock).toHaveBeenCalledTimes(1)
  })
})
