// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { render, screen, cleanup, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Routes, Route, useParams, useSearchParams } from 'react-router-dom'
import userEvent from '@testing-library/user-event'
import { ChatScreen } from './ChatScreen'
import type { ChatSource, Conversation, Message, MessageSource, Note, Template } from '../../../shared/types'

const {
  listConversationsMock,
  createConversationMock,
  listMessagesMock,
  sendMessageMock,
  listTemplatesMock,
  listNotesMock,
} = vi.hoisted(() => ({
  listConversationsMock: vi.fn(),
  createConversationMock: vi.fn(),
  listMessagesMock: vi.fn(),
  sendMessageMock: vi.fn(),
  listTemplatesMock: vi.fn(),
  listNotesMock: vi.fn(),
}))

vi.mock('@/api', () => ({
  muesli: {
    listConversations: listConversationsMock,
    createConversation: createConversationMock,
    listMessages: listMessagesMock,
    sendMessage: sendMessageMock,
    listTemplates: listTemplatesMock,
    listNotes: listNotesMock,
  },
}))

afterEach(cleanup)
beforeEach(() => {
  listConversationsMock.mockReset()
  createConversationMock.mockReset()
  listMessagesMock.mockReset().mockResolvedValue([])
  sendMessageMock.mockReset()
  listTemplatesMock.mockReset().mockResolvedValue([])
  listNotesMock.mockReset().mockResolvedValue([])
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

  // issue #765: cross-meeting analysis mode.
  describe('cross-meeting analysis', () => {
    const crossTemplate: Template = { id: 'tmpl-cross', name: 'Cross recap', phase: 'cross', sections: [], built_in: false, auto_run: false }
    const readyA: Note = {
      id: 'note-a', title: 'Sprint planning', status: 'ready', partial_transcript: false,
      created_at: '', updated_at: '',
    }
    const readyB: Note = {
      id: 'note-b', title: 'Retro', status: 'ready', partial_transcript: false,
      created_at: '', updated_at: '',
    }

    async function enterCrossMode() {
      listConversationsMock.mockResolvedValue([])
      listTemplatesMock.mockResolvedValue([crossTemplate])
      listNotesMock.mockResolvedValue([readyA, readyB])
      renderChatScreen()
      await screen.findByText(/start a new conversation/i)
      await userEvent.click(screen.getByRole('tab', { name: /cross-meeting analysis/i }))
      await screen.findByLabelText('Cross-analysis template')
    }

    async function selectTwoAndTemplate() {
      await userEvent.click(screen.getByRole('checkbox', { name: 'Sprint planning' }))
      await userEvent.click(screen.getByRole('checkbox', { name: 'Retro' }))
      await userEvent.selectOptions(screen.getByLabelText('Cross-analysis template'), 'tmpl-cross')
    }

    it('running analysis appends exactly one paired user+assistant message, rendering ordered sections and cross-note citations', async () => {
      await enterCrossMode()
      const sources: MessageSource[] = [
        { n: 1, note_id: 'note-a', transcript_generation: 1, segment_index: 0, timestamp: 0, snippet: 'a' },
        { n: 2, note_id: 'note-b', transcript_generation: 1, segment_index: 1, timestamp: 1000, snippet: 'b' },
      ]
      createConversationMock.mockResolvedValue({
        id: 'c-cross',
        title: '',
        created_at: '',
        updated_at: '',
        message: {
          id: 'm-cross',
          conversation_id: 'c-cross',
          role: 'assistant',
          content: '## Decisions\n\nShip Friday [1].\n\n## Risks\n\nDeadline slipped [2].',
          model: 'test-model',
          created_at: '',
          sources,
        },
      })

      await selectTwoAndTemplate()
      await userEvent.click(screen.getByRole('button', { name: /run analysis/i }))

      const log = await screen.findByRole('log')
      const headings = within(log).getAllByRole('heading', { level: 2 })
      expect(headings.map((h) => h.textContent)).toEqual(['Decisions', 'Risks'])
      // Exactly one user bubble + one assistant bubble.
      expect(screen.getByText(/Cross-meeting analysis using template/i)).toBeInTheDocument()
      expect(createConversationMock).toHaveBeenCalledTimes(1)
      const req = createConversationMock.mock.calls[0][0]
      expect(req.cross_analysis).toEqual({ template_id: 'tmpl-cross', note_ids: expect.arrayContaining(['note-a', 'note-b']) })
    })

    it('rolls back the optimistic user bubble and shows the chat error area on failure', async () => {
      await enterCrossMode()
      createConversationMock.mockRejectedValue(new Error('[400] fewer than two eligible notes'))

      await selectTwoAndTemplate()
      await userEvent.click(screen.getByRole('button', { name: /run analysis/i }))

      await screen.findByRole('alert')
      expect(screen.queryByText(/Cross-meeting analysis using template/i)).not.toBeInTheDocument()
    })

    it('a cross-analysis citation chip navigates to the cited note', async () => {
      await enterCrossMode()
      const sources: MessageSource[] = [{ n: 1, note_id: 'note-a', transcript_generation: 1, segment_index: 2, timestamp: 3000, snippet: 'a' }]
      createConversationMock.mockResolvedValue({
        id: 'c-cross',
        title: '',
        created_at: '',
        updated_at: '',
        message: {
          id: 'm-cross', conversation_id: 'c-cross', role: 'assistant',
          content: 'Ship Friday [1].', model: 'test-model', created_at: '', sources,
        },
      })

      await selectTwoAndTemplate()
      await userEvent.click(screen.getByRole('button', { name: /run analysis/i }))

      const chip = await screen.findByRole('button', { name: /citation 1/i })
      await userEvent.click(chip)
      expect(await screen.findByTestId('note-route')).toHaveTextContent('note=note-a segment_index=2')
    })

    it('sources persist and render correctly after reload (message.sources from listMessages)', async () => {
      listConversationsMock.mockResolvedValue([{ id: 'c-cross', title: 'Cross recap', created_at: '', updated_at: '' }])
      const sources: MessageSource[] = [{ n: 1, note_id: 'note-a', transcript_generation: 1, segment_index: 0, timestamp: 0, snippet: 'a' }]
      listMessagesMock.mockImplementation((id: string) =>
        id === 'c-cross'
          ? Promise.resolve([
              { id: 'm-cross', conversation_id: 'c-cross', role: 'assistant', content: 'Ship Friday [1].', model: 'test-model', created_at: '', sources },
            ])
          : Promise.resolve([]),
      )
      renderChatScreen()
      await userEvent.click(await screen.findByRole('button', { name: 'Cross recap' }))
      await waitFor(() => expect(listMessagesMock).toHaveBeenCalledWith('c-cross'))
      expect(await screen.findByRole('button', { name: /citation 1/i })).toBeInTheDocument()
    })
  })
})
