// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'

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
      <TagsPage />
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

  it('rename flow: click Rename, type new name, submit → renameTag called and list refreshed', async () => {
    listTags
      .mockResolvedValueOnce([{ id: 't1', name: 'alpha', count: 2 }])
      .mockResolvedValueOnce([{ id: 't1', name: 'beta', count: 2 }])

    renderPage()
    const renameBtn = await screen.findByRole('button', { name: 'Rename' })
    await userEvent.click(renameBtn)

    const input = screen.getByRole('textbox', { name: 'Tag name' })
    await userEvent.clear(input)
    await userEvent.type(input, 'beta')

    const saveBtn = screen.getByRole('button', { name: 'Save' })
    await userEvent.click(saveBtn)

    await waitFor(() => expect(renameTag).toHaveBeenCalledWith('t1', 'beta'))
    await waitFor(() => expect(listTags).toHaveBeenCalledTimes(2))
  })

  it('Escape cancels rename without calling renameTag', async () => {
    renderPage()
    const renameBtn = await screen.findByRole('button', { name: 'Rename' })
    await userEvent.click(renameBtn)

    screen.getByRole('textbox', { name: 'Tag name' })
    await userEvent.keyboard('{Escape}')

    expect(renameTag).not.toHaveBeenCalled()
    // Rename button should be visible again
    expect(await screen.findByRole('button', { name: 'Rename' })).toBeInTheDocument()
  })

  it('delete flow: click Delete, confirm → deleteTag called and list refreshed', async () => {
    listTags
      .mockResolvedValueOnce([{ id: 't1', name: 'alpha', count: 2 }])
      .mockResolvedValueOnce([])

    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true))

    renderPage()
    const deleteBtn = await screen.findByRole('button', { name: 'Delete alpha' })
    await userEvent.click(deleteBtn)

    await waitFor(() => expect(deleteTag).toHaveBeenCalledWith('t1'))
    await waitFor(() => expect(listTags).toHaveBeenCalledTimes(2))

    vi.unstubAllGlobals()
  })

  it('delete flow: cancel confirm → deleteTag not called', async () => {
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(false))

    renderPage()
    const deleteBtn = await screen.findByRole('button', { name: 'Delete alpha' })
    await userEvent.click(deleteBtn)

    expect(deleteTag).not.toHaveBeenCalled()

    vi.unstubAllGlobals()
  })
})
