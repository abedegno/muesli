import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { LivePromptRelay, SSEFrameParser } from './livePromptRelay'
import type { LivePromptsEvent } from '../shared/ipc'
import type { ServerConfig } from '../shared/types'

describe('SSEFrameParser', () => {
  it('parses a complete frame delivered in one chunk', () => {
    const frames: { event: string; data: string }[] = []
    const parser = new SSEFrameParser((f) => frames.push(f))
    parser.push(new TextEncoder().encode('event: snapshot\ndata: {"a":1}\n\n'))
    expect(frames).toEqual([{ event: 'snapshot', data: '{"a":1}' }])
  })

  it('parses a frame split at an arbitrary byte boundary, including mid-multibyte-character', () => {
    const frames: { event: string; data: string }[] = []
    const parser = new SSEFrameParser((f) => frames.push(f))
    const full = new TextEncoder().encode('event: update\ndata: {"t":"café"}\n\n')
    // Split in the middle of the multi-byte UTF-8 encoding of "é".
    const mid = full.length - 3
    parser.push(full.slice(0, mid))
    parser.push(full.slice(mid))
    expect(frames).toEqual([{ event: 'update', data: '{"t":"café"}' }])
  })

  it('isolates and drops a malformed frame (no event line) without affecting later frames', () => {
    const frames: { event: string; data: string }[] = []
    const parser = new SSEFrameParser((f) => frames.push(f))
    parser.push(new TextEncoder().encode('data: {"orphan":true}\n\nevent: update\ndata: {"ok":true}\n\n'))
    expect(frames).toEqual([{ event: 'update', data: '{"ok":true}' }])
  })

  it('parses two frames delivered in one chunk', () => {
    const frames: { event: string; data: string }[] = []
    const parser = new SSEFrameParser((f) => frames.push(f))
    parser.push(new TextEncoder().encode('event: a\ndata: 1\n\nevent: b\ndata: 2\n\n'))
    expect(frames.map((f) => f.event)).toEqual(['a', 'b'])
  })
})

function sseResponse(frames: string[], status = 200): Response {
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const frame of frames) controller.enqueue(new TextEncoder().encode(frame))
      controller.close()
    },
  })
  return new Response(stream, { status })
}

describe('LivePromptRelay', () => {
  const cfg: ServerConfig = { serverUrl: 'http://localhost:1234', token: 'tok' }
  let events: LivePromptsEvent[]

  beforeEach(() => {
    events = []
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  function emit(e: LivePromptsEvent) {
    events.push(e)
  }

  it('emits connecting, live, snapshot, and update events in order', async () => {
    const fetchImpl = vi.fn(async () =>
      sseResponse([
        'event: snapshot\ndata: {"note_id":"n1","stream_id":"s1","active":true,"items":[]}\n\n',
        'event: update\ndata: {"item":{"template_id":"t1","template_name":"T","stream_id":"s1","status":"ready","event_version":1,"rendered_revision":1,"desired_revision":1,"sections":[]}}\n\n',
      ]),
    )
    const relay = new LivePromptRelay({ getConfig: () => cfg, emit, fetchImpl: fetchImpl as unknown as typeof fetch })
    relay.start('n1')
    await vi.waitFor(() => expect(events.some((e) => e.type === 'update')).toBe(true))

    expect(events[0]).toEqual({ noteId: 'n1', type: 'connecting' })
    expect(events[1]).toEqual({ noteId: 'n1', type: 'live' })
    expect(events[2].type).toBe('snapshot')
    expect(events[3].type).toBe('update')
    expect(fetchImpl).toHaveBeenCalledWith(
      'http://localhost:1234/api/notes/n1/live-prompts',
      expect.objectContaining({ headers: { Authorization: 'Bearer tok' } }),
    )
  })

  it('ignores a duplicate or regressing event_version update after a snapshot', async () => {
    const fetchImpl = vi.fn(async () =>
      sseResponse([
        'event: snapshot\ndata: {"note_id":"n1","stream_id":"s1","active":true,"items":[{"template_id":"t1","template_name":"T","stream_id":"s1","status":"ready","event_version":3,"rendered_revision":1,"desired_revision":1,"sections":[]}]}\n\n',
        'event: update\ndata: {"item":{"template_id":"t1","template_name":"T","stream_id":"s1","status":"ready","event_version":2,"rendered_revision":1,"desired_revision":1,"sections":[]}}\n\n',
        'event: update\ndata: {"item":{"template_id":"t1","template_name":"T","stream_id":"s1","status":"ready","event_version":3,"rendered_revision":1,"desired_revision":1,"sections":[]}}\n\n',
        'event: update\ndata: {"item":{"template_id":"t1","template_name":"T","stream_id":"s1","status":"ready","event_version":4,"rendered_revision":2,"desired_revision":2,"sections":[]}}\n\n',
      ]),
    )
    const relay = new LivePromptRelay({ getConfig: () => cfg, emit, fetchImpl: fetchImpl as unknown as typeof fetch })
    relay.start('n1')
    await vi.waitFor(() => expect(events.filter((e) => e.type === 'update').length).toBe(1))

    const updates = events.filter((e) => e.type === 'update')
    expect(updates).toHaveLength(1)
    expect((updates[0] as { item: { event_version: number } }).item.event_version).toBe(4)
  })

  it('reconnects with capped exponential backoff on repeated connection failures, and stops cleanly', async () => {
    let calls = 0
    const fetchImpl = vi.fn(async () => {
      calls += 1
      throw new Error('connection refused')
    })
    const relay = new LivePromptRelay({ getConfig: () => cfg, emit, fetchImpl: fetchImpl as unknown as typeof fetch })
    relay.start('n1')
    await vi.advanceTimersByTimeAsync(0)
    expect(calls).toBe(1)

    // Before the 1s backoff elapses, no reconnect yet.
    await vi.advanceTimersByTimeAsync(999)
    expect(calls).toBe(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(calls).toBe(2)

    // Second backoff doubles to 2s.
    await vi.advanceTimersByTimeAsync(1999)
    expect(calls).toBe(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(calls).toBe(3)

    relay.stop('n1')
    const callsAtStop = calls
    await vi.advanceTimersByTimeAsync(60_000)
    expect(calls).toBe(callsAtStop) // cancelled timers stay cancelled
  })

  it('resets backoff to the initial delay after a successful connection', async () => {
    let calls = 0
    let fail = true
    const fetchImpl = vi.fn(async () => {
      calls += 1
      if (fail) throw new Error('connection refused')
      return sseResponse([])
    })
    const relay = new LivePromptRelay({ getConfig: () => cfg, emit, fetchImpl: fetchImpl as unknown as typeof fetch })
    relay.start('n1')
    await vi.advanceTimersByTimeAsync(0)
    expect(calls).toBe(1)
    await vi.advanceTimersByTimeAsync(1000) // first backoff (1s)
    expect(calls).toBe(2)

    // This attempt succeeds (then the stream ends immediately), which must
    // reset backoff back to the initial 1s rather than continuing to grow.
    fail = false
    await vi.advanceTimersByTimeAsync(2000) // second backoff would have been 2s
    expect(calls).toBe(3)
    fail = true
    await vi.advanceTimersByTimeAsync(999)
    expect(calls).toBe(3)
    await vi.advanceTimersByTimeAsync(1)
    expect(calls).toBe(4)
    relay.stop('n1')
  })

  it('emits dropped without a config, and never calls fetch', () => {
    const fetchImpl = vi.fn()
    const relay = new LivePromptRelay({ getConfig: () => null, emit, fetchImpl: fetchImpl as unknown as typeof fetch })
    relay.start('n1')
    expect(events).toEqual([{ noteId: 'n1', type: 'dropped' }])
    expect(fetchImpl).not.toHaveBeenCalled()
  })

  it('stop() aborts the in-flight request so the server can release its lease', async () => {
    let abortedSignal: AbortSignal | undefined
    const fetchImpl = vi.fn((_url: string, init?: RequestInit) => {
      abortedSignal = init?.signal ?? undefined
      return new Promise<Response>(() => {
        /* never resolves until aborted */
      })
    })
    const relay = new LivePromptRelay({ getConfig: () => cfg, emit, fetchImpl: fetchImpl as unknown as typeof fetch })
    relay.start('n1')
    await vi.waitFor(() => expect(fetchImpl).toHaveBeenCalled())
    relay.stop('n1')
    expect(abortedSignal?.aborted).toBe(true)
  })

  it('starting a new note stops the previous one (note scoping)', async () => {
    const fetchImpl = vi.fn(async (url: string) => { void url; return sseResponse([]) })
    const relay = new LivePromptRelay({ getConfig: () => cfg, emit, fetchImpl: fetchImpl as unknown as typeof fetch })
    relay.start('n1')
    await vi.waitFor(() => expect(fetchImpl).toHaveBeenCalledTimes(1))
    relay.start('n2')
    await vi.waitFor(() => expect(fetchImpl).toHaveBeenCalledTimes(2))
    expect(fetchImpl.mock.calls[1][0]).toContain('/notes/n2/')
  })
})
