// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ChatThread } from './ChatThread'
import type { ChatSource, Message } from '../../../shared/types'

afterEach(cleanup)

const userMsg: Message = {
  id: 'm1',
  conversation_id: 'c1',
  role: 'user',
  content: 'What did we decide about pricing?',
  model: '',
  created_at: '2024-01-01T00:00:00Z',
}

const assistantMsg: Message = {
  id: 'm2',
  conversation_id: 'c1',
  role: 'assistant',
  content: '## Pricing\n\nWe decided to go with tiered pricing [1].',
  model: 'gpt-test',
  created_at: '2024-01-01T00:00:05Z',
}

const sources: ChatSource[] = [
  { n: 1, note_id: 'note-1', segment_index: 3, timestamp: 12000, snippet: 'let\'s do tiered pricing' },
]

describe('ChatThread', () => {
  it('renders a user bubble and a markdown-rendered assistant bubble', () => {
    render(<ChatThread messages={[userMsg, assistantMsg]} sourcesByMessageId={{}} />)
    expect(screen.getByText('What did we decide about pricing?')).toBeInTheDocument()
    // The Markdown component renders "## Pricing" as an <h2>text</h2>.
    expect(screen.getByRole('heading', { level: 2, name: 'Pricing' })).toBeInTheDocument()
    expect(screen.getByText(/We decided to go with tiered pricing/)).toBeInTheDocument()
  })

  it('renders a valid [n] marker as a clickable citation chip carrying its source, and invokes onCiteClick with that source when clicked', async () => {
    const user = userEvent.setup()
    const onCiteClick = vi.fn()
    render(
      <ChatThread
        messages={[assistantMsg]}
        sourcesByMessageId={{ m2: sources }}
        onCiteClick={onCiteClick}
      />,
    )

    const chip = screen.getByRole('button', { name: /citation 1/i })
    expect(chip).toHaveTextContent('[1]')
    expect(chip).toHaveAttribute('title', "let's do tiered pricing")

    await user.click(chip)
    expect(onCiteClick).toHaveBeenCalledTimes(1)
    expect(onCiteClick).toHaveBeenCalledWith(sources[0])
  })

  it('degrades a [n] marker with no matching source to inert literal text, without crashing', () => {
    const onCiteClick = vi.fn()
    const msg: Message = { ...assistantMsg, id: 'm3', content: 'This cites a bad marker [7] and a real one [1].' }
    render(
      <ChatThread
        messages={[msg]}
        sourcesByMessageId={{ m3: sources }}
        onCiteClick={onCiteClick}
      />,
    )

    // [7] has no entry in sources -> plain text, not a button.
    expect(screen.queryByRole('button', { name: /citation 7/i })).not.toBeInTheDocument()
    expect(screen.getByText(/This cites a bad marker \[7\] and a real one/)).toBeInTheDocument()
    // [1] still resolves to a real chip.
    expect(screen.getByRole('button', { name: /citation 1/i })).toBeInTheDocument()
  })

  // Reproduces the known "history reload" gap (see PR description): CHT04's
  // listMessages doesn't persist/return per-message sources, so
  // useConversationThread's load effect resets sourcesByMessageId to `{}` for
  // every message pulled from history (see useConversationThread.ts's
  // `listMessages(...).then(... setSourcesByMessageId({}) ...)`), not just
  // for the ones whose specific marker number happens to be out of range. A
  // message reloaded from history with an otherwise-valid [1] marker has NO
  // entry in sourcesByMessageId at all -- this asserts that degrades to the
  // same inert-plain-text rendering (never a crash), which is the correct,
  // spec-compliant behaviour per contract point 3 given today's data
  // availability.
  it('degrades every [n] marker to literal text when sourcesByMessageId has no entry for the message at all (e.g. a history-reloaded message, not just an out-of-range marker number), without crashing', () => {
    const msg: Message = { ...assistantMsg, id: 'm4', content: 'No sources for this one [1].' }
    render(<ChatThread messages={[msg]} sourcesByMessageId={{}} />)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
    expect(screen.getByText(/No sources for this one \[1\]/)).toBeInTheDocument()
  })

  it('shows the empty-state label when there are no messages and not loading', () => {
    render(<ChatThread messages={[]} sourcesByMessageId={{}} emptyLabel="Ask a question to get started." />)
    expect(screen.getByText('Ask a question to get started.')).toBeInTheDocument()
  })

  it('shows a loading indicator instead of the empty state while loading', () => {
    render(<ChatThread messages={[]} sourcesByMessageId={{}} loading emptyLabel="Ask a question to get started." />)
    expect(screen.getByText(/loading messages/i)).toBeInTheDocument()
    expect(screen.queryByText('Ask a question to get started.')).not.toBeInTheDocument()
  })
})
