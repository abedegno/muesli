// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { FolderBar } from './FolderBar'
import type { Folder } from '../../shared/types'

afterEach(cleanup)

const folders: Folder[] = [
  { id: 'f1', name: 'Clients', created_at: '' },
  { id: 'f2', name: 'Q3', created_at: '' },
]

describe('FolderBar', () => {
  it("shows the note's folders as chips and removes one", async () => {
    const onRemove = vi.fn().mockResolvedValue(undefined)
    render(<FolderBar folders={folders} memberIds={['f1']} onAdd={vi.fn()} onCreate={vi.fn()} onRemove={onRemove} />)
    expect(screen.getByText('Clients')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /Remove Clients/ }))
    expect(onRemove).toHaveBeenCalledWith('f1')
  })

  it('adds an existing folder from the picker', async () => {
    const onAdd = vi.fn().mockResolvedValue(undefined)
    render(<FolderBar folders={folders} memberIds={['f1']} onAdd={onAdd} onCreate={vi.fn()} onRemove={vi.fn()} />)
    await userEvent.click(screen.getByRole('button', { name: /add to folder/i }))
    await userEvent.click(screen.getByRole('button', { name: 'Q3' })) // only non-member folders offered
    expect(onAdd).toHaveBeenCalledWith('f2')
  })

  it('closes the picker when picking an item (and calls onAdd)', async () => {
    const user = userEvent.setup()
    const onAdd = vi.fn().mockResolvedValue(undefined)
    render(<FolderBar folders={folders} memberIds={['f1']} onAdd={onAdd} onCreate={vi.fn()} onRemove={vi.fn()} />)
    await user.click(screen.getByRole('button', { name: /add to folder/i }))
    expect(screen.getByLabelText('New folder name')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Q3' }))
    expect(onAdd).toHaveBeenCalledWith('f2')
    expect(screen.queryByLabelText('New folder name')).not.toBeInTheDocument()
  })

  it('closes the picker when clicking outside (mousedown)', async () => {
    const user = userEvent.setup()
    render(<FolderBar folders={folders} memberIds={['f1']} onAdd={vi.fn()} onCreate={vi.fn()} onRemove={vi.fn()} />)
    await user.click(screen.getByRole('button', { name: /add to folder/i }))
    expect(screen.getByLabelText('New folder name')).toBeInTheDocument()
    // The mousedown listener is attached via setTimeout(0); wait until the
    // outside click actually closes the picker.
    await waitFor(() => {
      fireEvent.mouseDown(document.body)
      expect(screen.queryByLabelText('New folder name')).not.toBeInTheDocument()
    })
  })

  it('closes the picker when pressing Escape', async () => {
    const user = userEvent.setup()
    render(<FolderBar folders={folders} memberIds={['f1']} onAdd={vi.fn()} onCreate={vi.fn()} onRemove={vi.fn()} />)
    await user.click(screen.getByRole('button', { name: /add to folder/i }))
    expect(screen.getByLabelText('New folder name')).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByLabelText('New folder name')).not.toBeInTheDocument()
  })

  it('does not close the picker on an internal click', async () => {
    const user = userEvent.setup()
    render(<FolderBar folders={folders} memberIds={['f1']} onAdd={vi.fn()} onCreate={vi.fn()} onRemove={vi.fn()} />)
    await user.click(screen.getByRole('button', { name: /add to folder/i }))
    const input = screen.getByLabelText('New folder name')
    expect(input).toBeInTheDocument()
    // Click inside the dropdown (after the deferred listener is attached).
    await user.click(input)
    expect(screen.getByLabelText('New folder name')).toBeInTheDocument()
  })
})
