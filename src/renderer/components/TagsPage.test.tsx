// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { ToastProvider } from '@/components/ui/Toast'

const listTags = vi.fn().mockResolvedValue([{ id: 't1', name: 'alpha', count: 2 }])
const renameTag = vi.fn().mockResolvedValue({ id: 't1', name: 'beta' })
const deleteTag = vi.fn().mockResolvedValue(undefined)

vi.mock('@/api', () => ({
  muesli: {
    listTags: () => listTags(),
    renameTag: (id: string, name: string) => renameTag(id, name),
    deleteTag: (id: string) => deleteTag(id),
  },
}))

import { TagsPage } from './TagsPage'

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/settings/tags']}>
      <ToastProvider>
        <TagsPage />
      </ToastProvider>
    </MemoryRouter>,
  )
}

describe('TagsPage', () => {
  it('renders tag list with name and note count', async () => {
    renderPage()
    expect(await screen.findByText('alpha')).toBeInTheDocument()
    expect(screen.getByText(/2 notes/)).toBeInTheDocument()
  })

  it('shows "No tags yet." when list is empty', async () => {
    listTags.mockResolvedValueOnce([])
    renderPage()
    expect(await screen.findByText('No tags yet.')).toBeInTheDocument()
  })

  it('rename flow: clicking Rename opens the dialog, save calls renameTag, and closes it', async () => {
    listTags
      .mockResolvedValueOnce([{ id: 't1', name: 'alpha', count: 2 }])
      .mockResolvedValueOnce([{ id: 't1', name: 'beta', count: 2 }])

    renderPage()
    const user = userEvent.setup()
    const renameBtn = await screen.findByRole('button', { name: 'Rename' })
    await user.click(renameBtn)

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Rename tag')).toBeInTheDocument()

    const input = within(dialog).getByRole('textbox', { name: 'Tag name' })
    await user.clear(input)
    await user.type(input, 'beta')

    const saveBtn = within(dialog).getByRole('button', { name: 'Save' })
    await user.click(saveBtn)

    await waitFor(() => expect(renameTag).toHaveBeenCalledWith('t1', 'beta'))
    await waitFor(() => expect(listTags).toHaveBeenCalledTimes(2))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('Escape closes the rename dialog without calling renameTag', async () => {
    renderPage()
    const user = userEvent.setup()
    const renameBtn = await screen.findByRole('button', { name: 'Rename' })
    await user.click(renameBtn)

    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await user.keyboard('{Escape}')

    expect(renameTag).not.toHaveBeenCalled()
    expect(await screen.findByRole('button', { name: 'Rename' })).toBeInTheDocument()
  })

  it('delete flow: clicking Delete opens a themed confirm dialog and confirm deletes the tag', async () => {
    listTags
      .mockResolvedValueOnce([{ id: 't1', name: 'alpha', count: 2 }])
      .mockResolvedValueOnce([])

    renderPage()
    const user = userEvent.setup()
    const deleteBtn = await screen.findByRole('button', { name: 'Delete alpha' })
    await user.click(deleteBtn)

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText('Delete tag?')).toBeInTheDocument()
    expect(within(dialog).getByText(/Delete "alpha"\?/)).toBeInTheDocument()
    expect(within(dialog).getByText(/2 notes/)).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(deleteTag).toHaveBeenCalledWith('t1'))
    await waitFor(() => expect(listTags).toHaveBeenCalledTimes(2))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('delete flow: cancel closes the dialog without calling deleteTag', async () => {
    renderPage()
    const user = userEvent.setup()
    const deleteBtn = await screen.findByRole('button', { name: 'Delete alpha' })
    await user.click(deleteBtn)

    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    expect(deleteTag).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('rename failure shows a toast and keeps the dialog open', async () => {
    renameTag.mockRejectedValueOnce(new Error('Tag already exists'))

    renderPage()
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Rename' }))

    const dialog = screen.getByRole('dialog')
    const input = within(dialog).getByRole('textbox', { name: 'Tag name' })
    await user.clear(input)
    await user.type(input, 'beta')
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(screen.getByText('Tag already exists')).toBeInTheDocument())
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(renameTag).toHaveBeenCalledWith('t1', 'beta')
  })

  it('delete failure shows a toast and closes the dialog', async () => {
    deleteTag.mockRejectedValueOnce(new Error('Could not delete tag'))

    renderPage()
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Delete alpha' }))

    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Delete' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not delete tag')
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(deleteTag).toHaveBeenCalledWith('t1')
  })
})
