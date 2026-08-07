// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Note } from '../../shared/types'

vi.mock('./NoteListItem', () => ({
  NoteListItem: ({ note }: { note: Note }) => <span>{note.title}</span>,
}))
vi.mock('@/components/ui/Toast', () => ({ useToast: () => ({ notify: vi.fn() }) }))

import { GroupedNoteSections } from './NoteFeed'

const note = (id: string, title: string): Note => ({
  id,
  title,
  status: 'ready',
  created_at: '2026-07-09T09:00:00.000Z',
  updated_at: '2026-07-09T09:00:00.000Z',
  partial_transcript: false,
})

afterEach(cleanup)

describe('GroupedNoteSections', () => {
  it('renders nothing for an empty feed', () => {
    const { container } = render(
      <GroupedNoteSections groups={[]} folders={[]} refresh={vi.fn()} onOpenNote={vi.fn()} />,
    )
    expect(container).toBeEmptyDOMElement()
  })

  it('preserves group and note order', () => {
    render(
      <GroupedNoteSections
        groups={[
          { label: 'Pinned', notes: [note('1', 'First'), note('2', 'Second')] },
          { label: 'Today', notes: [note('3', 'Third')] },
        ]}
        folders={[]}
        refresh={vi.fn()}
        onOpenNote={vi.fn()}
      />,
    )

    expect(screen.getAllByRole('heading').map((heading) => heading.textContent)).toEqual(['Pinned', 'Today'])
    expect(screen.getAllByRole('listitem').map((item) => item.textContent)).toEqual(['First', 'Second', 'Third'])
  })

  it('passes the selected note to onOpenNote', async () => {
    const onOpenNote = vi.fn()
    const selected = note('2', 'Choose me')
    render(
      <GroupedNoteSections
        groups={[{ label: 'Today', notes: [note('1', 'Other'), selected] }]}
        folders={[]}
        refresh={vi.fn()}
        onOpenNote={onOpenNote}
      />,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Choose me' }))
    expect(onOpenNote).toHaveBeenCalledExactlyOnceWith(selected)
  })
})
