import { EventEmitter } from 'node:events'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

type FakeWebSocketOptions = {
  headers?: Record<string, string>
}

type FakeWebSocketInstance = EventEmitter & {
  url: string
  options?: FakeWebSocketOptions
  readyState: number
  sent: Buffer[]
  closeCalls: number
  open: () => void
  fail: (err?: Error) => void
}

const websocketState = vi.hoisted(() => {
  const websocketInstances: FakeWebSocketInstance[] = []
  const websocketCalls: Array<{ url: string; options?: FakeWebSocketOptions }> = []
  return { websocketCalls, websocketInstances }
})

vi.mock('ws', () => ({
  default: class FakeWebSocket extends EventEmitter {
    static readonly OPEN = 1
    static readonly CONNECTING = 0
    static readonly CLOSING = 2
    static readonly CLOSED = 3

    public readyState = FakeWebSocket.CONNECTING
    public sent: Buffer[] = []
    public closeCalls = 0

    constructor(
      public readonly url: string,
      public readonly options?: FakeWebSocketOptions,
    ) {
      super()
      websocketState.websocketInstances.push(this as unknown as FakeWebSocketInstance)
      websocketState.websocketCalls.push({ url, options })
    }

    send(data: Buffer) {
      this.sent.push(Buffer.from(data))
    }

    close() {
      this.closeCalls++
      this.readyState = FakeWebSocket.CLOSED
      this.emit('close')
    }

    open() {
      this.readyState = FakeWebSocket.OPEN
      this.emit('open')
    }

    fail(err = new Error('transport error')) {
      this.emit('error', err)
    }
  },
}))

async function loadRelay() {
  return await import('./noteStreamRelay')
}

function makeRelay(config: { serverUrl: string; token: string } | null = { serverUrl: 'http://server', token: 'secret' }) {
  const emit = vi.fn()
  const getConfig = vi.fn(() => config)
  return {
    emit,
    getConfig,
    relayFactory: async () => {
      const { NoteStreamRelay } = await loadRelay()
      return new NoteStreamRelay({ getConfig, emit })
    },
  }
}

describe('NoteStreamRelay', () => {
  beforeEach(() => {
    websocketState.websocketCalls.length = 0
    websocketState.websocketInstances.length = 0
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('forwards queued and live chunks in order to the server client', async () => {
    const { emit, relayFactory } = makeRelay()
    const relay = await relayFactory()

    relay.start('note-1')
    relay.sendAudio('note-1', Uint8Array.from([1, 2]).buffer)
    relay.sendAudio('note-1', Uint8Array.from([3]).buffer)

    expect(emit).toHaveBeenCalledWith({ noteId: 'note-1', type: 'connecting' })
    expect(websocketState.websocketCalls).toHaveLength(1)
    expect(websocketState.websocketCalls[0]).toEqual({
      url: 'http://server/api/notes/note-1/stream',
      options: {
        headers: {
          Authorization: 'Bearer secret',
        },
      },
    })

    const socket = websocketState.websocketInstances[0]
    socket.open()

    relay.sendAudio('note-1', Uint8Array.from([4, 5, 6]).buffer)

    expect(emit).toHaveBeenCalledWith({ noteId: 'note-1', type: 'live' })
    expect(socket.sent).toEqual([
      Buffer.from([1, 2]),
      Buffer.from([3]),
      Buffer.from([4, 5, 6]),
    ])
  })

  it('ignores unknown or already-closed notes without throwing or forwarding', async () => {
    const { relayFactory } = makeRelay()
    const relay = await relayFactory()

    expect(() => relay.sendAudio('missing', Uint8Array.from([9]).buffer)).not.toThrow()

    relay.start('note-1')
    const socket = websocketState.websocketInstances[0]
    socket.open()
    relay.stop('note-1')

    expect(socket.closeCalls).toBe(1)
    expect(() => relay.sendAudio('note-1', Uint8Array.from([8]).buffer)).not.toThrow()
    expect(socket.sent).toEqual([])
  })

  it('surfaces transport errors as dropped and leaves the stream inert afterward', async () => {
    const { emit, relayFactory } = makeRelay()
    const relay = await relayFactory()

    relay.start('note-1')
    const socket = websocketState.websocketInstances[0]
    socket.open()
    relay.sendAudio('note-1', Uint8Array.from([7]).buffer)
    socket.fail(new Error('socket broke'))

    expect(emit).toHaveBeenCalledWith({ noteId: 'note-1', type: 'dropped' })

    relay.sendAudio('note-1', Uint8Array.from([8]).buffer)
    expect(socket.sent).toEqual([Buffer.from([7])])
    expect(socket.closeCalls).toBe(0)
  })

  it('releases the active stream on stop and makes later sends no-ops', async () => {
    const { relayFactory } = makeRelay()
    const relay = await relayFactory()

    relay.start('note-1')
    const socket = websocketState.websocketInstances[0]
    socket.open()

    relay.stop('note-1')

    expect(socket.closeCalls).toBe(1)

    relay.sendAudio('note-1', Uint8Array.from([1]).buffer)
    expect(socket.sent).toEqual([])
  })

  it('keeps concurrent notes isolated from one another', async () => {
    const { relayFactory } = makeRelay()
    const relay = await relayFactory()

    relay.start('note-1')
    const firstSocket = websocketState.websocketInstances[0]
    firstSocket.open()
    relay.sendAudio('note-1', Uint8Array.from([1]).buffer)

    relay.start('note-2')
    const secondSocket = websocketState.websocketInstances[1]
    secondSocket.open()

    expect(firstSocket.closeCalls).toBe(1)

    relay.sendAudio('note-1', Uint8Array.from([2]).buffer)
    relay.sendAudio('note-2', Uint8Array.from([3]).buffer)

    expect(firstSocket.sent).toEqual([Buffer.from([1])])
    expect(secondSocket.sent).toEqual([Buffer.from([3])])
  })
})
