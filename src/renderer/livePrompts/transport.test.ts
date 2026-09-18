// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { installContextBridgeLike } from '../test-utils/bridge'
import type { LivePromptsEvent } from '../../shared/ipc'

async function loadTransport() {
  vi.resetModules()
  return import('./transport')
}

describe('subscribeLivePrompts', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  it('starts the subscription and forwards events scoped to this note', async () => {
    let listener: ((event: LivePromptsEvent) => void) | undefined
    const startLivePrompts = vi.fn(async () => undefined)
    const stopLivePrompts = vi.fn(async () => undefined)
    const onLivePromptsEvent = vi.fn((cb: (event: LivePromptsEvent) => void) => {
      listener = cb
      return () => {
        listener = undefined
      }
    })
    installContextBridgeLike({ startLivePrompts, stopLivePrompts, onLivePromptsEvent })

    const { subscribeLivePrompts } = await loadTransport()
    const received: LivePromptsEvent[] = []
    const unsubscribe = subscribeLivePrompts('note-1', (e) => received.push(e))

    expect(startLivePrompts).toHaveBeenCalledWith('note-1')
    expect(onLivePromptsEvent).toHaveBeenCalledTimes(1)

    listener?.({ noteId: 'note-1', type: 'live' })
    listener?.({ noteId: 'other-note', type: 'live' }) // wrong note: must be ignored
    expect(received).toEqual([{ noteId: 'note-1', type: 'live' }])

    unsubscribe()
    expect(stopLivePrompts).toHaveBeenCalledWith('note-1')
    expect(listener).toBeUndefined()

    // Events after unsubscribe are never forwarded, even if something still
    // holds a reference to the callback.
    received.length = 0
    onLivePromptsEvent.mock.calls[0][0]({ noteId: 'note-1', type: 'live' })
    expect(received).toEqual([])
  })

  it('unsubscribe is idempotent', async () => {
    const stopLivePrompts = vi.fn(async () => undefined)
    installContextBridgeLike({
      startLivePrompts: vi.fn(async () => undefined),
      stopLivePrompts,
      onLivePromptsEvent: vi.fn(() => () => {}),
    })
    const { subscribeLivePrompts } = await loadTransport()
    const unsubscribe = subscribeLivePrompts('note-1', () => {})
    unsubscribe()
    unsubscribe()
    expect(stopLivePrompts).toHaveBeenCalledTimes(1)
  })
})
