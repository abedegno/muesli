// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TagRenameDialog } from './TagRenameDialog'

afterEach(cleanup)

describe('TagRenameDialog', () => {
  it('prefills the current tag name', () => {
    render(<TagRenameDialog open initialName="Meeting Notes" onSave={vi.fn()} onClose={vi.fn()} />)

    expect(screen.getByLabelText('Tag name')).toHaveValue('Meeting Notes')
  })

  it('saves a trimmed new name and closes on success', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    const onClose = vi.fn()

    render(<TagRenameDialog open initialName="Meeting Notes" onSave={onSave} onClose={onClose} />)

    const input = screen.getByLabelText('Tag name')
    await userEvent.clear(input)
    await userEvent.type(input, '  Project Alpha  ')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(onSave).toHaveBeenCalledWith('Project Alpha')
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('disables save for a blank name', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)

    render(<TagRenameDialog open initialName="Meeting Notes" onSave={onSave} onClose={vi.fn()} />)

    const input = screen.getByLabelText('Tag name')
    const save = screen.getByRole('button', { name: 'Save' })

    await userEvent.clear(input)

    expect(save).toBeDisabled()
    await userEvent.click(save)
    expect(onSave).not.toHaveBeenCalled()
  })

  it('keeps the dialog open when save fails', async () => {
    const onSave = vi.fn().mockRejectedValue(new Error('duplicate tag'))
    const onClose = vi.fn()

    render(<TagRenameDialog open initialName="Meeting Notes" onSave={onSave} onClose={onClose} />)

    await userEvent.clear(screen.getByLabelText('Tag name'))
    await userEvent.type(screen.getByLabelText('Tag name'), 'Meeting Notes')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(onSave).toHaveBeenCalledWith('Meeting Notes')
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByLabelText('Tag name')).toBeInTheDocument()
  })

  it('cancels without saving', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    const onClose = vi.fn()

    render(<TagRenameDialog open initialName="Meeting Notes" onSave={onSave} onClose={onClose} />)

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(onClose).toHaveBeenCalledTimes(1)
    expect(onSave).not.toHaveBeenCalled()
  })
})
