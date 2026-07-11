// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { FolderDialog } from './FolderDialog'

afterEach(cleanup)

describe('FolderDialog', () => {
  it('creates a folder (Save disabled until non-empty)', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<FolderDialog open title="New folder" onSave={onSave} onClose={() => {}} />)
    const save = screen.getByRole('button', { name: 'Save' })
    expect(save).toBeDisabled()
    await userEvent.type(screen.getByLabelText('Folder name'), 'Clients')
    await userEvent.click(save)
    expect(onSave).toHaveBeenCalledWith('Clients', null)
  })

  it('passes the selected parent folder to onSave', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(
      <FolderDialog open title="New folder" onSave={onSave} onClose={() => {}}
        parentOptions={[{ id: 'p1', name: 'Clients' }]} />,
    )
    await userEvent.type(screen.getByLabelText('Folder name'), 'Acme')
    await userEvent.selectOptions(screen.getByLabelText('Parent folder'), 'p1')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave).toHaveBeenCalledWith('Acme', 'p1')
  })

  it('shows Move to Trash in edit mode and fires it', async () => {
    const onDelete = vi.fn().mockResolvedValue(undefined)
    render(<FolderDialog open title="Edit folder" initialName="Clients" onSave={vi.fn()} onDelete={onDelete} onClose={() => {}} />)
    await userEvent.click(screen.getByRole('button', { name: 'Move to Trash' }))
    expect(onDelete).toHaveBeenCalled()
  })

  it('hides Move to Trash in create mode', () => {
    render(<FolderDialog open title="New folder" onSave={vi.fn()} onClose={() => {}} />)
    expect(screen.queryByRole('button', { name: 'Move to Trash' })).not.toBeInTheDocument()
  })
})
