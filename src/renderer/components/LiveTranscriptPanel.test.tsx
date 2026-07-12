// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import type { NoteStreamEvent } from '../../shared/ipc'
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

    act(() => {
      source.emit({ noteId: 'note-1', type: 'connecting' })
      source.emit({ noteId: 'note-1', type: 'live' })
      source.emit({
        noteId: 'note-1',
        type: 'segment',
        text: 'first',
        start_ms: 0,
        end_ms: 200,
        speaker: null,
        provisional: true,
      })
      source.emit({
        noteId: 'note-1',
        type: 'segment',
        text: 'second',
        start_ms: 200,
        end_ms: 400,
        speaker: null,
        provisional: true,
      })
    })

    expect(source.events).toContainEqual({
      noteId: 'note-1',
      type: 'segment',
      text: 'first',
      start_ms: 0,
      end_ms: 200,
      speaker: null,
      provisional: true,
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
})
