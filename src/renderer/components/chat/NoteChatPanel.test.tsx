// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { NoteChatPanel } from './NoteChatPanel'
import type { Conversation, Message } from '../../../shared/types'

const {
  listConversationsMock,
  createConversationMock,
  listMessagesMock,
  sendMessageMock,
} = vi.hoisted(() => ({
  listConversationsMock: vi.fn(),
  createConversationMock: vi.fn(),
  listMessagesMock: vi.fn(),
  sendMessageMock: vi.fn(),
}))

vi.mock('@/api', () => ({
  muesli: {
    listConversations: listConversationsMock,
    createConversation: createConversationMock,
    listMessages: listMessagesMock,
    sendMessage: sendMessageMock,
  },
}))

afterEach(cleanup)
beforeEach(() => {
  listConversationsMock.mockReset().mockResolvedValue([])
  createConversationMock.mockReset()
  listMessagesMock.mockReset().mockResolvedValue([])
  sendMessageMock.mockReset()
})

// A promise you can resolve/reject from the outside, to control pending timing.
function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

const conversation: Conversation = {
  id: 'c1',
  note_id: 'note-1',
  title: 'About this note',
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
}

describe('NoteChatPanel', () => {
  it('sends the first message on a fresh note (create-and-send), showing a pending state then the reply', async () => {
    const d = deferred<{ id: string; note_id: string; title: string; created_at: string; updated_at: string; message: Message; sources: [] }>()
    createConversationMock.mockReturnValue(d.promise)

    render(<NoteChatPanel noteId="note-1" onClose={() => {}} />)
    await waitFor(() => expect(listConversationsMock).toHaveBeenCalledWith('note-1'))

    const input = await screen.findByRole('textbox', { name: /message/i })
    await userEvent.type(input, 'What were the action items?')
    await userEvent.click(screen.getByRole('button', { name: /^send$/i }))

    // Pending: input disabled + a visible sending indicator.
    expect(input).toBeDisabled()
    expect(screen.getByRole('button', { name: /sending/i })).toBeInTheDocument()

    d.resolve({
      id: 'c1',
      note_id: 'note-1',
      title: 'What were the action items?',
      created_at: '',
      updated_at: '',
      message: {
        id: 'm-assistant',
        conversation_id: 'c1',
        role: 'assistant',
        content: 'The action items were **X** and **Y**.',
        model: 'gpt-test',
        created_at: '',
      },
      sources: [],
    })

    await waitFor(() => expect(input).not.toBeDisabled())
    expect(screen.getByText('What were the action items?')).toBeInTheDocument()
    expect(screen.getByText(/The action items were/)).toBeInTheDocument()
    expect(createConversationMock).toHaveBeenCalledWith(
      expect.objectContaining({ note_id: 'note-1', content: 'What were the action items?' }),
    )
  })

  it('surfaces a 409 in-flight send distinctly from a generic error, re-enabling input either way', async () => {
    listConversationsMock.mockResolvedValue([conversation])
    listMessagesMock.mockResolvedValue([])
    sendMessageMock.mockRejectedValueOnce(new Error('[409] message send already in progress'))

    render(<NoteChatPanel noteId="note-1" onClose={() => {}} />)
    const input = await screen.findByRole('textbox', { name: /message/i })

    await userEvent.type(input, 'Second question')
    await userEvent.click(screen.getByRole('button', { name: /^send$/i }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/already sending/i))
    expect(input).not.toBeDisabled()

    // A different (non-409) failure surfaces the server's generic/plugin-failure
    // message instead — distinct wording, and never crashes the thread view.
    sendMessageMock.mockRejectedValueOnce(new Error('[500] internal error'))
    await userEvent.type(input, 'Second question')
    await userEvent.click(screen.getByRole('button', { name: /^send$/i }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('internal error'))
    expect(screen.queryByText(/already sending/i)).not.toBeInTheDocument()
    expect(input).not.toBeDisabled()
  })
})
