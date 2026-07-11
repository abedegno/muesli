// @vitest-environment jsdom
import { describe, it, expect, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import { NoteListItem } from './NoteListItem'
import type { Note, Folder } from '../../shared/types'

afterEach(cleanup)

const note: Note = {
  id: '1', title: 'Standup', status: 'ready',
  created_at: new Date(2026, 5, 13, 17, 15).toISOString(), updated_at: '', snippet: 'auth to prod', partial_transcript: false,
}

describe('NoteListItem', () => {
  it('renders the monogram initial, title, snippet, status and time', () => {
    render(<NoteListItem note={note} />)
    expect(screen.getByText('S')).toBeInTheDocument()       // monogram
    expect(screen.getByText('Standup')).toBeInTheDocument()
    expect(screen.getByText('auth to prod')).toBeInTheDocument()
    expect(screen.getByText(/ready/i)).toBeInTheDocument()
    expect(screen.getByText(/5:15/)).toBeInTheDocument()    // time
  })

  it('renders a chip per tag when the note has tags', () => {
    render(<NoteListItem note={{ ...note, tags: ['auth', 'urgent'] }} />)
    expect(screen.getByText('auth')).toBeInTheDocument()
    expect(screen.getByText('urgent')).toBeInTheDocument()
  })

  it('renders no tag chips when tags are absent or empty', () => {
    const { rerender } = render(<NoteListItem note={note} />)
    expect(screen.queryByText('auth')).toBeNull()
    expect(screen.queryByText('urgent')).toBeNull()

    rerender(<NoteListItem note={{ ...note, tags: [] }} />)
    expect(screen.queryByText('auth')).toBeNull()
    expect(screen.queryByText('urgent')).toBeNull()
  })

  it('renders a folder chip when the note has folder_ids that resolve in the folders prop', () => {
    const folders: Folder[] = [{ id: 'f1', name: 'Work', created_at: '' }]
    render(<NoteListItem note={{ ...note, folder_ids: ['f1'] }} folders={folders} />)
    expect(screen.getByText('Work')).toBeInTheDocument()
  })

  it('shows a pinned indicator when the note is pinned', () => {
    render(<NoteListItem note={{ ...note, pinned: true }} />)
    expect(screen.getByLabelText('Pinned')).toBeInTheDocument()
  })

  it('renders no folder chips when folder_ids is absent or empty', () => {
    const folders: Folder[] = [{ id: 'f1', name: 'Work', created_at: '' }]

    const { rerender } = render(<NoteListItem note={note} folders={folders} />)
    expect(screen.queryByText('Work')).toBeNull()

    rerender(<NoteListItem note={{ ...note, folder_ids: [] }} folders={folders} />)
    expect(screen.queryByText('Work')).toBeNull()
  })
})
