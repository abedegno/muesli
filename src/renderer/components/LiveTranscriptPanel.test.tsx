// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import type { NoteStreamEvent, NoteStreamSegmentEvent } from '../../shared/ipc'
import { LiveTranscriptPanel } from './LiveTranscriptPanel'

function createSource() {
  const listeners = new Set<(event: NoteStreamEvent) => void>()
  const events: NoteStreamEvent[] = []
  return {
    onNoteStreamEvent(cb: (event: NoteStreamEvent) => void) {
      listeners.add(cb)
      return () => listeners.delete(cb)
    },
    emit(event: NoteStreamEvent) {
      events.push(event)
      listeners.forEach((cb) => cb(event))
    },
    events,
  }
}

describe('LiveTranscriptPanel', () => {
  const scrollIntoView = vi.fn()

  beforeEach(() => {
    scrollIntoView.mockClear()
    Element.prototype.scrollIntoView = scrollIntoView
  })

  afterEach(() => {
    cleanup()
  })

  it('appends finalized segments in arrival order and auto-scrolls to the latest', async () => {
    const source = createSource()
    render(<LiveTranscriptPanel noteId="note-1" isRecording source={source} />)
    const firstSegment: NoteStreamSegmentEvent = {
      noteId: 'note-1',
      type: 'segment',
      text: 'first',
      start_ms: 0,
      end_ms: 200,
      speaker: null,
      provisional: true,
      final: true,
    }
    const secondSegment: NoteStreamSegmentEvent = {
      noteId: 'note-1',
      type: 'segment',
      text: 'second',
      start_ms: 200,
      end_ms: 400,
      speaker: null,
      provisional: true,
      final: true,
    }

    act(() => {
      source.emit({ noteId: 'note-1', type: 'connecting' })
      source.emit({ noteId: 'note-1', type: 'live' })
      source.emit(firstSegment)
      source.emit(secondSegment)
    })

    expect(source.events).toContainEqual({
      noteId: 'note-1',
      type: 'segment',
      text: 'first',
      start_ms: 0,
      end_ms: 200,
      speaker: null,
      provisional: true,
      final: true,
    })

    expect(await screen.findByTestId('live-transcript-panel')).toBeInTheDocument()
    expect(screen.getByText('Live')).toBeInTheDocument()
    expect(screen.getByText('first')).toBeInTheDocument()
    expect(screen.getByText('second')).toBeInTheDocument()
    expect(screen.getByText('first').compareDocumentPosition(screen.getByText('second')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    await waitFor(() => {
      expect(scrollIntoView).toHaveBeenCalled()
    })
  })

  it('keeps only the latest interim segment until a final commit arrives', async () => {
    const source = createSource()
    render(<LiveTranscriptPanel noteId="note-1" isRecording source={source} />)

    const partial = (text: string): NoteStreamSegmentEvent => ({
      noteId: 'note-1',
      type: 'segment',
      text,
      start_ms: 0,
      end_ms: 100,
      speaker: null,
      provisional: true,
      final: false,
    })

    act(() => {
      source.emit({ noteId: 'note-1', type: 'connecting' })
      source.emit({ noteId: 'note-1', type: 'live' })
      source.emit(partial('he'))
    })

    expect(await screen.findByTestId('live-transcript-interim')).toHaveTextContent('he')
    expect(screen.queryAllByText('he')).toHaveLength(1)
    expect(screen.queryAllByText('he', { selector: 'p' })).toHaveLength(1)

    act(() => {
      source.emit(partial('hel'))
    })

    expect(screen.getByTestId('live-transcript-interim')).toHaveTextContent('hel')
    expect(screen.queryAllByText('hel', { selector: 'p' })).toHaveLength(1)
    expect(screen.queryByText('he')).not.toBeInTheDocument()

    act(() => {
      source.emit(partial('hello'))
    })

    expect(screen.getByTestId('live-transcript-interim')).toHaveTextContent('hello')
    expect(screen.queryAllByText('hello', { selector: 'p' })).toHaveLength(1)

    act(() => {
      source.emit({
        noteId: 'note-1',
        type: 'segment',
        text: 'hello',
        start_ms: 0,
        end_ms: 100,
        speaker: null,
        provisional: true,
        final: true,
      })
    })

    expect(screen.queryByTestId('live-transcript-interim')).not.toBeInTheDocument()
    expect(screen.getAllByText('hello', { selector: 'p' })).toHaveLength(1)
  })

  it('shows the quiet degradation note and hides the live panel when unavailable', async () => {
    const source = createSource()
    render(<LiveTranscriptPanel noteId="note-1" isRecording source={source} />)

    act(() => {
      source.emit({ noteId: 'note-1', type: 'unavailable' })
    })

    expect(await screen.findByTestId('live-transcript-unavailable')).toHaveTextContent(
      'Live transcript unavailable — the full transcript will be ready after recording.',
    )
    expect(screen.queryByTestId('live-transcript-panel')).not.toBeInTheDocument()

    cleanup()
    render(<LiveTranscriptPanel noteId="note-1" isRecording source={source} />)
    act(() => {
      source.emit({ noteId: 'note-1', type: 'dropped' })
    })
    expect(await screen.findByTestId('live-transcript-unavailable')).toBeInTheDocument()
    expect(screen.queryByTestId('live-transcript-panel')).not.toBeInTheDocument()
  })

  it('stays visible with distinct startup copy while loading, then transitions to listening and segments', async () => {
	const source = createSource()
	render(<LiveTranscriptPanel noteId="note-1" isRecording source={source} />)

	act(() => source.emit({ noteId: 'note-1', type: 'loading' }))

	expect(await screen.findByTestId('live-transcript-panel')).toBeVisible()
	expect(screen.getByText(/Starting the transcription engine/)).toBeInTheDocument()
	expect(screen.queryByText('Listening…')).not.toBeInTheDocument()

	act(() => source.emit({ noteId: 'note-1', type: 'live' }))
	expect(screen.getByText('Listening…')).toBeInTheDocument()
	expect(screen.queryByText(/Starting the transcription engine/)).not.toBeInTheDocument()

	act(() => source.emit({
		noteId: 'note-1', type: 'segment', text: 'Ready now', start_ms: 0, end_ms: 100,
		speaker: null, provisional: true, final: true,
	}))
	expect(screen.getByText('Ready now')).toBeInTheDocument()
  })
})
