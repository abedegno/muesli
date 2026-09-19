// @vitest-environment jsdom
import { act, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { LivePromptsPanel } from './LivePromptsPanel'
import type { LivePromptsEvent, LivePromptsItem } from './transport'

function makeItem(overrides: Partial<LivePromptsItem> = {}): LivePromptsItem {
  return {
    template_id: 't1',
    template_name: 'Action items',
    stream_id: 's1',
    status: 'pending',
    event_version: 1,
    rendered_revision: 0,
    desired_revision: 1,
    sections: [],
    ...overrides,
  }
}

function fakeSubscribe(script: (emit: (e: LivePromptsEvent) => void) => void) {
  return (_noteId: string, callback: (event: LivePromptsEvent) => void) => {
    script(callback)
    return () => {}
  }
}

describe('LivePromptsPanel', () => {
  it('renders nothing while not recording', () => {
    const subscribe = fakeSubscribe(() => {})
    render(<LivePromptsPanel noteId="n1" isRecording={false} subscribe={subscribe} />)
    expect(screen.queryByTestId('live-prompts-panel')).not.toBeInTheDocument()
  })

  it('shows "waiting for speech" before any result', () => {
    const subscribe = fakeSubscribe((emit) => {
      emit({ noteId: 'n1', type: 'snapshot', streamId: 's1', active: true, items: [makeItem({ status: 'pending' })] })
    })
    render(<LivePromptsPanel noteId="n1" isRecording subscribe={subscribe} />)
    expect(screen.getByTestId('live-prompt-waiting-t1')).toHaveTextContent('Waiting for speech')
  })

  it('shows "generating" while running with no prior content', () => {
    const subscribe = fakeSubscribe((emit) => {
      emit({ noteId: 'n1', type: 'snapshot', streamId: 's1', active: true, items: [makeItem({ status: 'running' })] })
    })
    render(<LivePromptsPanel noteId="n1" isRecording subscribe={subscribe} />)
    expect(screen.getByTestId('live-prompt-waiting-t1')).toHaveTextContent('Generating')
  })

  it('renders ready sections and replaces them atomically on the next ready update', async () => {
    let push: (e: LivePromptsEvent) => void = () => {}
    const subscribe = fakeSubscribe((emit) => {
      push = emit
      emit({
        noteId: 'n1',
        type: 'snapshot',
        streamId: 's1',
        active: true,
        items: [makeItem({ status: 'ready', sections: [{ heading: 'Actions', content_markdown: 'First pass' }] })],
      })
    })
    render(<LivePromptsPanel noteId="n1" isRecording subscribe={subscribe} />)
    expect(screen.getByTestId('live-prompt-card-t1')).toHaveTextContent('First pass')

    act(() => {
      push({
        noteId: 'n1',
        type: 'update',
        item: makeItem({ status: 'ready', event_version: 2, sections: [{ heading: 'Actions', content_markdown: 'Second pass' }] }),
      })
    })
    await waitFor(() => expect(screen.getByTestId('live-prompt-card-t1')).toHaveTextContent('Second pass'))
    expect(screen.getByTestId('live-prompt-card-t1')).not.toHaveTextContent('First pass')
  })

  it('retains prior content and labels "Updating." while refreshing', () => {
    const subscribe = fakeSubscribe((emit) => {
      emit({
        noteId: 'n1',
        type: 'snapshot',
        streamId: 's1',
        active: true,
        items: [makeItem({ status: 'running', sections: [{ heading: 'Actions', content_markdown: 'Existing' }] })],
      })
    })
    render(<LivePromptsPanel noteId="n1" isRecording subscribe={subscribe} />)
    expect(screen.getByTestId('live-prompt-card-t1')).toHaveTextContent('Existing')
    expect(screen.getByTestId('live-prompt-footer-t1')).toHaveTextContent('Updating.')
  })

  it('failure with no prior content explains it will retry', () => {
    const subscribe = fakeSubscribe((emit) => {
      emit({ noteId: 'n1', type: 'snapshot', streamId: 's1', active: true, items: [makeItem({ status: 'failed' })] })
    })
    render(<LivePromptsPanel noteId="n1" isRecording subscribe={subscribe} />)
    expect(screen.getByTestId('live-prompt-failed-t1')).toHaveTextContent('It will retry when the transcript grows.')
  })

  it('failure with prior content retains it and labels "Not updated."', () => {
    const subscribe = fakeSubscribe((emit) => {
      emit({
        noteId: 'n1',
        type: 'snapshot',
        streamId: 's1',
        active: true,
        items: [makeItem({ status: 'failed', sections: [{ heading: 'Actions', content_markdown: 'Kept' }] })],
      })
    })
    render(<LivePromptsPanel noteId="n1" isRecording subscribe={subscribe} />)
    expect(screen.getByTestId('live-prompt-card-t1')).toHaveTextContent('Kept')
    expect(screen.getByTestId('live-prompt-footer-t1')).toHaveTextContent('Not updated.')
  })

  it('removes a card immediately on an "ended" event', async () => {
    let push: (e: LivePromptsEvent) => void = () => {}
    const subscribe = fakeSubscribe((emit) => {
      push = emit
      emit({
        noteId: 'n1',
        type: 'snapshot',
        streamId: 's1',
        active: true,
        items: [makeItem({ status: 'ready', sections: [{ heading: 'A', content_markdown: 'x' }] })],
      })
    })
    render(<LivePromptsPanel noteId="n1" isRecording subscribe={subscribe} />)
    expect(screen.getByTestId('live-prompt-card-t1')).toBeInTheDocument()
    act(() => {
      push({ noteId: 'n1', type: 'ended', templateId: 't1', streamId: 's1' })
    })
    await waitFor(() => expect(screen.queryByTestId('live-prompt-card-t1')).not.toBeInTheDocument())
  })

  it('preserves stable order across an update', () => {
    let push: (e: LivePromptsEvent) => void = () => {}
    const subscribe = fakeSubscribe((emit) => {
      push = emit
      emit({
        noteId: 'n1',
        type: 'snapshot',
        streamId: 's1',
        active: true,
        items: [makeItem({ template_id: 'a', template_name: 'A' }), makeItem({ template_id: 'b', template_name: 'B' })],
      })
    })
    render(<LivePromptsPanel noteId="n1" isRecording subscribe={subscribe} />)
    const before = screen.getAllByTestId(/live-prompt-card-/).map((el) => el.getAttribute('data-testid'))
    act(() => {
      push({
        noteId: 'n1',
        type: 'update',
        item: makeItem({
          template_id: 'a',
          template_name: 'A',
          status: 'ready',
          event_version: 2,
          sections: [{ heading: 'A', content_markdown: 'x' }],
        }),
      })
    })
    const after = screen.getAllByTestId(/live-prompt-card-/).map((el) => el.getAttribute('data-testid'))
    expect(after).toEqual(before)
  })

  it('announces only template name and "updated" on a ready transition', async () => {
    let push: (e: LivePromptsEvent) => void = () => {}
    const subscribe = fakeSubscribe((emit) => {
      push = emit
      emit({ noteId: 'n1', type: 'snapshot', streamId: 's1', active: true, items: [makeItem({ status: 'running' })] })
    })
    render(<LivePromptsPanel noteId="n1" isRecording subscribe={subscribe} />)
    act(() => {
      push({
        noteId: 'n1',
        type: 'update',
        item: makeItem({ status: 'ready', event_version: 2, sections: [{ heading: 'A', content_markdown: 'x' }] }),
      })
    })
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Action items updated.'))
  })
})
