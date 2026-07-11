// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ActivityFeed } from './ActivityFeed'
import type { ActivityItem } from '@/lib/activityStore'

// ---------------------------------------------------------------------------
// Mock useActivity so we can control items + dismiss without a real provider.
// ---------------------------------------------------------------------------

const mockDismiss = vi.fn()
let mockItems: ActivityItem[] = []

vi.mock('@/lib/activityStore', () => ({
  useActivity: () => ({ items: mockItems, dismiss: mockDismiss }),
}))

afterEach(() => {
  cleanup()
  mockDismiss.mockClear()
  mockItems = []
})

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ActivityFeed — empty state', () => {
  it('returns null when items list is empty', () => {
    mockItems = []
    const { container } = render(<ActivityFeed />)
    expect(container.firstChild).toBeNull()
  })
})

describe('ActivityFeed — upload item', () => {
  it('renders an upload item with the correct human-readable label', () => {
    mockItems = [
      {
        kind: 'upload',
        id: 'n1',
        noteId: 'n1',
        noteTitle: 'Team Standup',
        phase: 'uploading-audio',
        done: false,
      },
    ]
    render(<ActivityFeed />)
    expect(screen.getByText('Team Standup — Uploading audio…')).toBeInTheDocument()
  })

  it('maps all upload phases to human-readable labels', () => {
    const phases: Array<{ phase: import('@/lib/activityStore').UploadPhase; label: string }> = [
      { phase: 'requesting-url', label: 'Preparing upload…' },
      { phase: 'uploading-audio', label: 'Uploading audio…' },
      { phase: 'confirming-upload', label: 'Confirming…' },
      { phase: 'done', label: 'Upload complete' },
      { phase: 'error', label: 'Upload failed' },
    ]
    for (const { phase, label } of phases) {
      mockItems = [
        { kind: 'upload', id: 'n1', noteId: 'n1', noteTitle: 'Note', phase, done: phase === 'done' },
      ]
      const { unmount } = render(<ActivityFeed />)
      expect(screen.getByText(`Note — ${label}`)).toBeInTheDocument()
      unmount()
    }
  })

  it('shows a ✓ indicator when the upload item is done', () => {
    mockItems = [
      {
        kind: 'upload',
        id: 'n1',
        noteId: 'n1',
        noteTitle: 'Meeting',
        phase: 'done',
        done: true,
      },
    ]
    render(<ActivityFeed />)
    expect(screen.getByLabelText('complete')).toBeInTheDocument()
  })
})

describe('ActivityFeed — processing item', () => {
  it('renders a processing item with the correct label from statusLabel()', () => {
    mockItems = [
      {
        kind: 'processing',
        id: 'n2',
        noteId: 'n2',
        noteTitle: 'Sprint Retro',
        status: 'transcribing',
        done: false,
      },
    ]
    render(<ActivityFeed />)
    // statusLabel('transcribing') returns 'Transcribing'
    expect(screen.getByText('Sprint Retro — Transcribing')).toBeInTheDocument()
  })
})

describe('ActivityFeed — dismiss button', () => {
  it('calls dismiss with the item id when × is clicked', async () => {
    mockItems = [
      {
        kind: 'upload',
        id: 'n1',
        noteId: 'n1',
        noteTitle: 'Meeting',
        phase: 'uploading-audio',
        done: false,
      },
    ]
    const user = userEvent.setup()
    render(<ActivityFeed />)
    await user.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(mockDismiss).toHaveBeenCalledWith('n1')
  })
})

describe('ActivityFeed — multiple items', () => {
  it('renders all items when multiple are present', () => {
    mockItems = [
      {
        kind: 'upload',
        id: 'n1',
        noteId: 'n1',
        noteTitle: 'Note A',
        phase: 'uploading-audio',
        done: false,
      },
      {
        kind: 'processing',
        id: 'n2',
        noteId: 'n2',
        noteTitle: 'Note B',
        status: 'summarizing',
        done: false,
      },
      {
        kind: 'processing',
        id: 'n3',
        noteId: 'n3',
        noteTitle: 'Note C',
        status: 'ready',
        done: true,
      },
    ]
    render(<ActivityFeed />)
    expect(screen.getByText('Note A — Uploading audio…')).toBeInTheDocument()
    expect(screen.getByText('Note B — Summarizing')).toBeInTheDocument()
    expect(screen.getByText('Note C — Ready')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: 'Dismiss' })).toHaveLength(3)
  })
})
