// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route, useParams, useSearchParams } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { ChatScreen } from './ChatScreen'
import type { ChatSource, Conversation, Message } from '../../../shared/types'

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
  listConversationsMock.mockReset()
  createConversationMock.mockReset()
  listMessagesMock.mockReset().mockResolvedValue([])
  sendMessageMock.mockReset()
})

const conversations: Conversation[] = [
  { id: 'c1', title: 'Roadmap planning', created_at: '', updated_at: '' },
  { id: 'c2', title: 'Budget review', created_at: '', updated_at: '' },
]

// Route stub that surfaces the resolved note id + `segment_index` query param
// so a citation-chip click's navigation target is observable end-to-end,
// mirroring NotesListScreen.test.tsx's NoteRouteStub for `?segment=`.
function NoteRouteStub() {
  const { id } = useParams()
  const [params] = useSearchParams()
  return <div data-testid="note-route">{`note=${id ?? ''} segment_index=${params.get('segment_index') ?? ''}`}</div>
}

function renderChatScreen() {
  return render(
    <MemoryRouter initialEntries={['/chat']}>
      <Routes>
        <Route path="/chat" element={<ChatScreen />} />
        <Route path="/notes/:id" element={<NoteRouteStub />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('ChatScreen', () => {
  it('lists existing conversations and loads the selected one’s messages', async () => {
    listConversationsMock.mockResolvedValue(conversations)
    const msgs: Message[] = [
      { id: 'm1', conversation_id: 'c2', role: 'user', content: 'What is the Q3 budget?', model: '', created_at: '' },
    ]
    listMessagesMock.mockImplementation((id: string) => (id === 'c2' ? Promise.resolve(msgs) : Promise.resolve([])))

    renderChatScreen()
    expect(await screen.findByRole('button', { name: 'Roadmap planning' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Budget review' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Budget review' }))
    await waitFor(() => expect(listMessagesMock).toHaveBeenCalledWith('c2'))
    expect(await screen.findByText('What is the Q3 budget?')).toBeInTheDocument()
  })

  it('starting a new conversation sends create-and-send and selects it in the list', async () => {
    listConversationsMock.mockResolvedValue([])
    createConversationMock.mockResolvedValue({
      id: 'c-new',
      title: 'Any blockers this week?',
      created_at: '',
      updated_at: '',
      message: {
        id: 'm-reply',
        conversation_id: 'c-new',
        role: 'assistant',
        content: 'No blockers reported.',
        model: 'gpt-test',
        created_at: '',
      },
      sources: [],
    })

    renderChatScreen()
    await screen.findByText(/start a new conversation/i)

    const input = screen.getByRole('textbox', { name: /message/i })
    await userEvent.type(input, 'Any blockers this week?')
    await userEvent.click(screen.getByRole('button', { name: /^send$/i }))

    expect(await screen.findByText('No blockers reported.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Any blockers this week?' })).toBeInTheDocument()
  })

  it('clicking an inline citation chip navigates to the cited note with a `segment_index` query param', async () => {
    listConversationsMock.mockResolvedValue(conversations)
    const source: ChatSource = { n: 1, note_id: 'note-9', segment_index: 4, timestamp: 5000, snippet: 'the cited snippet' }
    const msgs: Message[] = [
      { id: 'm1', conversation_id: 'c1', role: 'assistant', content: 'Per the transcript [1].', model: 'gpt-test', created_at: '' },
    ]
    listMessagesMock.mockImplementation((id: string) => (id === 'c1' ? Promise.resolve(msgs) : Promise.resolve([])))
    // useConversationThread only populates sourcesByMessageId from a send()
    // response, not from listMessages -- so send a follow-up turn whose
    // response carries `sources`, then click the chip on that reply.
    sendMessageMock.mockResolvedValue({
      message: { id: 'm2', conversation_id: 'c1', role: 'assistant', content: 'See [1] for details.', model: 'gpt-test', created_at: '' },
      sources: [source],
    })

    renderChatScreen()
    await userEvent.click(await screen.findByRole('button', { name: 'Roadmap planning' }))
    await waitFor(() => expect(listMessagesMock).toHaveBeenCalledWith('c1'))

    const input = screen.getByRole('textbox', { name: /message/i })
    await userEvent.type(input, 'Follow up question')
    await userEvent.click(screen.getByRole('button', { name: /^send$/i }))

    const chip = await screen.findByRole('button', { name: /citation 1/i })
    await userEvent.click(chip)

    expect(await screen.findByTestId('note-route')).toHaveTextContent('note=note-9 segment_index=4')
  })
})
