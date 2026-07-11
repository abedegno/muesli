// @vitest-environment jsdom
import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import type { Folder, Note, SmartList } from '../../shared/types'

const listTrash = vi.fn()
const restoreNote = vi.fn()
const permanentDeleteNote = vi.fn()
const listTrashedFolders = vi.fn()
const restoreFolder = vi.fn()
const permanentDeleteFolder = vi.fn()
const listTrashedSmartLists = vi.fn()
const restoreSmartList = vi.fn()
const permanentDeleteSmartList = vi.fn()

vi.mock('@/api', () => ({
  muesli: {
    listTrash: () => listTrash(),
    restoreNote: (id: string) => restoreNote(id),
    permanentDeleteNote: (id: string) => permanentDeleteNote(id),
    listTrashedFolders: () => listTrashedFolders(),
    restoreFolder: (id: string) => restoreFolder(id),
    permanentDeleteFolder: (id: string) => permanentDeleteFolder(id),
    listTrashedSmartLists: () => listTrashedSmartLists(),
    restoreSmartList: (id: string) => restoreSmartList(id),
    permanentDeleteSmartList: (id: string) => permanentDeleteSmartList(id),
  },
}))

import { TrashScreen } from './TrashScreen'

beforeEach(() => {
  // Default: no trashed folders/lists unless a test overrides it. Note tests assume both empty.
  listTrashedFolders.mockResolvedValue([])
  listTrashedSmartLists.mockResolvedValue([])
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

function renderScreen() {
  return render(
    <MemoryRouter initialEntries={['/trash']}>
      <Routes>
        <Route path="/trash" element={<TrashScreen />} />
      </Routes>
    </MemoryRouter>,
  )
}

const note = (over: Partial<Note> = {}): Note => ({
  id: 'n1', title: 'Deleted standup', status: 'ready', created_at: '', updated_at: '', partial_transcript: false, ...over,
} as Note)

const folder = (over: Partial<Folder> = {}): Folder => ({
  id: 'f1', name: 'Deleted clients', parent_id: null, created_at: '', ...over,
})

const smartList = (over: Partial<SmartList> = {}): SmartList => ({
  id: 'l1', name: 'Deleted weekly', created_at: '', rule: { op: 'and', children: [] }, ...over,
})

describe('TrashScreen', () => {
  it('lists a trashed note and restores it', async () => {
    listTrash.mockResolvedValueOnce([note()]).mockResolvedValueOnce([])
    restoreNote.mockResolvedValue(undefined)
    renderScreen()

    expect(await screen.findByText('Deleted standup')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /restore/i }))

    expect(restoreNote).toHaveBeenCalledWith('n1')
    // After restore it reloads (second listTrash returns empty).
    await waitFor(() => expect(listTrash).toHaveBeenCalledTimes(2))
    expect(await screen.findByText(/trash is empty/i)).toBeInTheDocument()
  })

  it('shows the empty state when there is no trash', async () => {
    listTrash.mockResolvedValue([])
    renderScreen()
    expect(await screen.findByText(/trash is empty/i)).toBeInTheDocument()
  })

  it('permanently deletes a note after confirming', async () => {
    listTrash.mockResolvedValueOnce([note()]).mockResolvedValueOnce([])
    permanentDeleteNote.mockResolvedValue(undefined)
    renderScreen()

    await screen.findByText('Deleted standup')
    await userEvent.click(screen.getByRole('button', { name: /delete forever/i }))
    // Confirm in the dialog (second "Delete forever" button).
    const confirmButtons = screen.getAllByRole('button', { name: /delete forever/i })
    await userEvent.click(confirmButtons[confirmButtons.length - 1])

    expect(permanentDeleteNote).toHaveBeenCalledWith('n1')
    await waitFor(() => expect(listTrash).toHaveBeenCalledTimes(2))
  })

  it('lists a trashed folder and restores it', async () => {
    listTrash.mockResolvedValue([])
    listTrashedFolders.mockResolvedValueOnce([folder()]).mockResolvedValueOnce([])
    restoreFolder.mockResolvedValue(undefined)
    renderScreen()

    expect(await screen.findByText('Deleted clients')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /restore/i }))

    expect(restoreFolder).toHaveBeenCalledWith('f1')
    // After restore it reloads both lists.
    await waitFor(() => expect(listTrashedFolders).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(listTrash).toHaveBeenCalledTimes(2))
  })

  it('permanently deletes a folder after confirming', async () => {
    listTrash.mockResolvedValue([])
    listTrashedFolders.mockResolvedValueOnce([folder()]).mockResolvedValueOnce([])
    permanentDeleteFolder.mockResolvedValue(undefined)
    renderScreen()

    await screen.findByText('Deleted clients')
    await userEvent.click(screen.getByRole('button', { name: /delete forever/i }))
    // Confirm in the dialog (second "Delete forever" button).
    const confirmButtons = screen.getAllByRole('button', { name: /delete forever/i })
    await userEvent.click(confirmButtons[confirmButtons.length - 1])

    expect(permanentDeleteFolder).toHaveBeenCalledWith('f1')
    await waitFor(() => expect(listTrashedFolders).toHaveBeenCalledTimes(2))
  })

  it('lists a trashed smart list and restores it', async () => {
    listTrash.mockResolvedValue([])
    listTrashedSmartLists.mockResolvedValueOnce([smartList()]).mockResolvedValueOnce([])
    restoreSmartList.mockResolvedValue(undefined)
    renderScreen()

    expect(await screen.findByText('Deleted weekly')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /restore/i }))

    expect(restoreSmartList).toHaveBeenCalledWith('l1')
    // After restore it reloads all lists.
    await waitFor(() => expect(listTrashedSmartLists).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(listTrash).toHaveBeenCalledTimes(2))
  })

  it('permanently deletes a smart list after confirming', async () => {
    listTrash.mockResolvedValue([])
    listTrashedSmartLists.mockResolvedValueOnce([smartList()]).mockResolvedValueOnce([])
    permanentDeleteSmartList.mockResolvedValue(undefined)
    renderScreen()

    await screen.findByText('Deleted weekly')
    await userEvent.click(screen.getByRole('button', { name: /delete forever/i }))
    // Confirm in the dialog (second "Delete forever" button).
    const confirmButtons = screen.getAllByRole('button', { name: /delete forever/i })
    await userEvent.click(confirmButtons[confirmButtons.length - 1])

    expect(permanentDeleteSmartList).toHaveBeenCalledWith('l1')
    await waitFor(() => expect(listTrashedSmartLists).toHaveBeenCalledTimes(2))
  })

  it('shows the empty state only when folders, lists, and notes are all empty', async () => {
    // Folders present, notes/lists empty: not empty.
    listTrash.mockResolvedValue([])
    listTrashedFolders.mockResolvedValue([folder()])
    renderScreen()

    expect(await screen.findByText('Deleted clients')).toBeInTheDocument()
    expect(screen.queryByText(/trash is empty/i)).not.toBeInTheDocument()
  })

  it('shows the smart list when only a list is trashed', async () => {
    // Lists present, notes/folders empty: not empty.
    listTrash.mockResolvedValue([])
    listTrashedSmartLists.mockResolvedValue([smartList()])
    renderScreen()

    expect(await screen.findByText('Deleted weekly')).toBeInTheDocument()
    expect(screen.queryByText(/trash is empty/i)).not.toBeInTheDocument()
  })

  it('renders all three sections when all item types are trashed', async () => {
    listTrash.mockResolvedValue([note()])
    listTrashedFolders.mockResolvedValue([folder()])
    listTrashedSmartLists.mockResolvedValue([smartList()])
    renderScreen()

    // All three section headings must appear.
    expect(await screen.findByText(/notes/i)).toBeInTheDocument()
    expect(screen.getByText(/folders/i)).toBeInTheDocument()
    expect(screen.getByText(/smart lists/i)).toBeInTheDocument()

    // One item from each section must be present.
    expect(screen.getByText('Deleted standup')).toBeInTheDocument()  // note title
    expect(screen.getByText('Deleted clients')).toBeInTheDocument()  // folder name
    expect(screen.getByText('Deleted weekly')).toBeInTheDocument()   // smart list name
  })

  it('clicking delete-forever shows the confirm dialog without deleting', async () => {
    listTrash.mockResolvedValue([note()])
    renderScreen()

    await screen.findByText('Deleted standup')
    // Click the item-level "Delete forever" button.
    await userEvent.click(screen.getByRole('button', { name: /delete forever/i }))

    // The confirm dialog must be visible.
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    // The purge IPC must NOT have been called yet.
    expect(permanentDeleteNote).not.toHaveBeenCalled()
  })
})

describe('TrashScreen — deletion date and days remaining', () => {
  it('shows "Deleted N days ago" and "M days left" for a note trashed 3 days ago', async () => {
    const threeDaysAgo = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString()
    listTrash.mockResolvedValue([note({ deleted_at: threeDaysAgo })])
    renderScreen()

    // "3 days ago" should appear
    expect(await screen.findByText(/3 days ago/)).toBeInTheDocument()
    // "27 days left" should appear (30 − 3 = 27)
    expect(screen.getByText(/27 days left/)).toBeInTheDocument()
  })

  it('shows "Deleted N days ago" and "M days left" for a folder trashed 5 days ago', async () => {
    listTrash.mockResolvedValue([])
    const fiveDaysAgo = new Date(Date.now() - 5 * 24 * 60 * 60 * 1000).toISOString()
    listTrashedFolders.mockResolvedValue([folder({ deleted_at: fiveDaysAgo })])
    renderScreen()

    // "5 days ago" should appear
    expect(await screen.findByText(/5 days ago/)).toBeInTheDocument()
    // "25 days left" (30 − 5 = 25)
    expect(screen.getByText(/25 days left/)).toBeInTheDocument()
  })

  it('shows "Deleted N days ago" and "M days left" for a smart list trashed 10 days ago', async () => {
    listTrash.mockResolvedValue([])
    const tenDaysAgo = new Date(Date.now() - 10 * 24 * 60 * 60 * 1000).toISOString()
    listTrashedSmartLists.mockResolvedValue([smartList({ deleted_at: tenDaysAgo })])
    renderScreen()

    // "10 days ago" should appear
    expect(await screen.findByText(/10 days ago/)).toBeInTheDocument()
    // "20 days left" (30 − 10 = 20)
    expect(screen.getByText(/20 days left/)).toBeInTheDocument()
  })

  it('renders nothing extra when deleted_at is absent', async () => {
    // note() has no deleted_at — no "days ago" or "days left" should appear
    listTrash.mockResolvedValue([note()])
    renderScreen()

    await screen.findByText('Deleted standup')
    expect(screen.queryByText(/days ago/)).not.toBeInTheDocument()
    expect(screen.queryByText(/days left/)).not.toBeInTheDocument()
  })
})

describe('TrashScreen — Delete forever dialog copy (EXT04)', () => {
  it('shows privacy-forward clarifying copy about exported files when note delete-forever dialog is open', async () => {
    listTrash.mockResolvedValue([note()])
    renderScreen()

    await screen.findByText('Deleted standup')
    // Open the delete-forever dialog for the note
    await userEvent.click(screen.getByRole('button', { name: /delete forever/i }))

    // The confirm dialog should be open — assert the new copy is present
    expect(screen.getByText(/Any files you've previously exported to disk are not affected/)).toBeInTheDocument()
    // permanentDeleteNote must NOT have fired yet
    expect(permanentDeleteNote).not.toHaveBeenCalled()
  })
})
