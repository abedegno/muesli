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

function makeReview(reviewState = 'pending'): DiarizationReview {
  return {
    note_id: 'n1',
    review_state: reviewState,
    turns: [
      { id: 't1', start_ms: 0, end_ms: 1000, text: 'turn one text', source: 'mixed', speaker: 'SPEAKER_00', confidence: 0.2 },
      { id: 't2', start_ms: 1000, end_ms: 2000, text: 'turn two text', source: 'mixed', speaker: 'SPEAKER_01', confidence: 0.4 },
      { id: 't3', start_ms: 2000, end_ms: 3000, text: 'turn three text', source: 'mixed', speaker: 'SPEAKER_00', confidence: 0.6 },
    ],
  }
}

beforeEach(() => {
  getDiarizationReviewMock.mockReset().mockResolvedValue(makeReview())
  postDiarizationReviewMock.mockReset().mockImplementation(async (_noteId: string, body: { reviewState?: string }) =>
    makeReview(body.reviewState ?? 'in_review'))
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
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { reviewState: 'in_review' }),
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
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't1', speaker: 'SPEAKER_00' }),
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
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't2', speaker: 'SPEAKER_00' }),
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
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't1', speaker: 'SPEAKER_01' }),
    )
  })

  it('Accept all remaining confirms every not-yet-confirmed turn, skipping already-confirmed ones', async () => {
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()
    const options = screen.getAllByRole('option')

    // Confirm the first turn individually first.
    fireEvent.keyDown(options[0], { key: 'Enter' })
    await waitFor(() =>
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't1', speaker: 'SPEAKER_00' }),
    )
    postDiarizationReviewMock.mockClear()

    await userEvent.click(screen.getByRole('button', { name: /accept all remaining/i }))

    await waitFor(() => {
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't2', speaker: 'SPEAKER_01' })
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { segmentId: 't3', speaker: 'SPEAKER_00' })
    })
    // The already-confirmed turn must NOT be re-posted by the bulk action.
    expect(postDiarizationReviewMock).not.toHaveBeenCalledWith('n1', { segmentId: 't1', speaker: 'SPEAKER_00' })
    expect(postDiarizationReviewMock).toHaveBeenCalledTimes(2)
  })

  it('marking the review complete posts review_state=completed and hides the entry point', async () => {
    render(<DiarizationReviewPanel noteId="n1" hasTranscript />)
    await openPanel()

    await userEvent.click(screen.getByRole('button', { name: /mark review complete/i }))

    await waitFor(() =>
      expect(postDiarizationReviewMock).toHaveBeenCalledWith('n1', { reviewState: 'completed' }),
    )
    await waitFor(() => expect(screen.queryByRole('button', { name: /review speakers/i })).not.toBeInTheDocument())
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
