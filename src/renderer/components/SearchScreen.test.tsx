// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { SearchScreen } from './SearchScreen'
import type { Folder, Note, PersonWithCompany, SearchMatch } from '../../shared/types'

const { searchMock, listPeopleMock, listFoldersMock, listNotesMock, navigateMock } = vi.hoisted(() => ({
  searchMock: vi.fn(),
  listPeopleMock: vi.fn(),
  listFoldersMock: vi.fn(),
  listNotesMock: vi.fn(),
  navigateMock: vi.fn(),
}))

vi.mock('react-router-dom', () => ({
  useNavigate: () => navigateMock,
}))

vi.mock('@/api', () => ({
  muesli: {
    search: (...args: unknown[]) => searchMock(...args),
    listPeople: () => listPeopleMock(),
    listFolders: () => listFoldersMock(),
    listNotes: () => listNotesMock(),
  },
}))

afterEach(cleanup)

beforeEach(() => {
  searchMock.mockReset()
  listPeopleMock.mockReset()
  listFoldersMock.mockReset()
  listNotesMock.mockReset()
  navigateMock.mockReset()
})

function person(over: Partial<PersonWithCompany> = {}): PersonWithCompany {
  return {
    id: 'p1',
    primary_email: 'alex@example.com',
    display_name: 'Alex Doe',
    first_seen_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-02T00:00:00Z',
    ...over,
  }
}

function folder(over: Partial<Folder> = {}): Folder {
  return {
    id: 'f1',
    name: 'Planning',
    created_at: '2026-07-01T00:00:00Z',
    ...over,
  }
}

function note(over: Partial<Note> = {}): Note {
  return {
    id: 'n1',
    title: 'Weekly planning',
    status: 'ready',
    created_at: '2026-07-02T00:00:00Z',
    updated_at: '2026-07-02T00:00:00Z',
    partial_transcript: false,
    folder_ids: [],
    tags: [],
    pinned: false,
    ...over,
  }
}

function match(over: Partial<SearchMatch> = {}): SearchMatch {
  return {
    note_id: 'n1',
    match_type: 'title',
    ...over,
  }
}

describe('SearchScreen', () => {
  it('applies filters, clears them, and shows the empty state when there are no matches', async () => {
    listPeopleMock.mockResolvedValue([person()])
    listFoldersMock.mockResolvedValue([folder()])
    listNotesMock.mockResolvedValue([note()])
    searchMock
      .mockResolvedValueOnce([match({ match_type: 'title' })])
      .mockResolvedValueOnce([match({ match_type: 'summary', snippet: 'budget review' })])
      .mockResolvedValueOnce([])

    const user = userEvent.setup()
    render(<SearchScreen />)

    expect(screen.getByText('Search your notes')).toBeInTheDocument()

    fireEvent.change(screen.getByRole('textbox', { name: /search query/i }), { target: { value: 'budget' } })

    await waitFor(() => {
      expect(searchMock).toHaveBeenCalledWith('budget', expect.objectContaining({ personId: undefined }))
    })

    await user.selectOptions(screen.getByRole('combobox', { name: /search person/i }), 'p1')

    await waitFor(() => {
      expect(searchMock).toHaveBeenLastCalledWith(
        'budget',
        expect.objectContaining({ personId: 'p1', folderId: undefined, tag: undefined }),
      )
    })

    expect(await screen.findByText('Weekly planning')).toBeInTheDocument()
    expect(screen.getByText('1 match')).toBeInTheDocument()
    expect(screen.getByText('Summary')).toBeInTheDocument()
    expect(screen.getByText('budget review')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /clear filters/i }))

    await waitFor(() => {
      expect(searchMock).toHaveBeenLastCalledWith(
        'budget',
        expect.objectContaining({ from: undefined, to: undefined, personId: undefined, folderId: undefined, tag: undefined }),
      )
    })

    expect(await screen.findByText('No matches')).toBeInTheDocument()
    expect(screen.getByText('Try a broader query or clear filters to search again.')).toBeInTheDocument()
  })
})
