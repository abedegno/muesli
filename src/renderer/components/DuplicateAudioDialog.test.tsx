// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DuplicateAudioDialog } from './DuplicateAudioDialog'

afterEach(cleanup)

describe('DuplicateAudioDialog', () => {
  it('renders the message and both buttons', () => {
    render(
      <DuplicateAudioDialog
        existingNoteTitle="My Standup"
        onOpenExisting={vi.fn()}
        onTranscribeAgain={vi.fn()}
      />,
    )

    expect(
      screen.getByText('This looks like an existing recording — open it, or transcribe again?'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Open existing recording' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Transcribe again' })).toBeInTheDocument()
  })

  it('clicking "Open existing recording" calls onOpenExisting', async () => {
    const onOpenExisting = vi.fn()
    render(
      <DuplicateAudioDialog
        existingNoteTitle="My Standup"
        onOpenExisting={onOpenExisting}
        onTranscribeAgain={vi.fn()}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Open existing recording' }))

    expect(onOpenExisting).toHaveBeenCalledTimes(1)
  })

  it('clicking "Transcribe again" calls onTranscribeAgain', async () => {
    const onTranscribeAgain = vi.fn()
    render(
      <DuplicateAudioDialog
        existingNoteTitle="My Standup"
        onOpenExisting={vi.fn()}
        onTranscribeAgain={onTranscribeAgain}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Transcribe again' }))

    expect(onTranscribeAgain).toHaveBeenCalledTimes(1)
  })
})
