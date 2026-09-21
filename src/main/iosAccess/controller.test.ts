import { PassThrough } from 'node:stream'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { IOSAccessController, MAX_CONTROL_LINE_BYTES, ProcessControlChannel, type ControlChannel } from './controller'
import type { RawInterface } from './networkCandidates'

class FakeChannel implements ControlChannel {
  sent: string[] = []
  private lineHandlers: Array<(line: string) => void> = []
  private closeHandlers: Array<() => void> = []
  closed = false

  send(line: string): void {
    this.sent.push(line)
  }
  onLine(handler: (line: string) => void): void {
    this.lineHandlers.push(handler)
  }
  onClose(handler: () => void): void {
    this.closeHandlers.push(handler)
  }
  // --- test helpers, not part of ControlChannel ---
  emitLine(line: string): void {
    for (const h of this.lineHandlers) h(line)
  }
  emitClose(): void {
    this.closed = true
    for (const h of this.closeHandlers) h()
  }
  lastRequest(): Record<string, unknown> {
    return JSON.parse(this.sent[this.sent.length - 1]) as Record<string, unknown>
  }
}

const en0Pair = { interfaceName: 'en0', address: '192.168.1.20', family: 'ipv4' as const }

describe('IOSAccessController.enumerate', () => {
  it('delegates to eligibleCandidates/evaluateSelection over injected interfaces', () => {
    const interfaces: RawInterface[] = [{ name: 'en0', addresses: ['192.168.1.20/24'] }]
    const controller = new IOSAccessController(new FakeChannel(), { listInterfaces: () => interfaces })
    const outcome = controller.enumerate()
    expect(outcome.candidates).toEqual([en0Pair])
    expect(outcome.autoSelected).toEqual(en0Pair)
  })
})

describe('IOSAccessController.enable', () => {
  it('sends a well-formed request and resolves with the pairing display', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)

    const promise = controller.enable(en0Pair)
    expect(channel.lastRequest()).toEqual({
      id: 'req-1',
      action: 'enable',
      pair: { InterfaceName: 'en0', Address: '192.168.1.20', Family: 'ipv4' },
    })

    channel.emitLine(
      JSON.stringify({
        id: 'req-1',
        ok: true,
        port: 54321,
        origin: 'https://192.168.1.20:54321',
        fingerprint_sha256: 'ab'.repeat(32),
        phrase: 'AB-CD',
      }),
    )

    const result = await promise
    expect(result.origin).toBe('https://192.168.1.20:54321')
    expect(result.port).toBe(54321)
    expect(result.phrase).toBe('AB-CD')
    expect(result.qrPayload).toContain('"origin":"https://192.168.1.20:54321"')
    expect(result.qrPayload).not.toMatch(/password|token|session/i)
  })

  it('rejects on an error response', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)
    const promise = controller.enable(en0Pair)
    channel.emitLine(JSON.stringify({ id: 'req-1', ok: false, error: 'candidate gone' }))
    await expect(promise).rejects.toThrow('candidate gone')
  })

  it('coalesces concurrent enable calls onto one in-flight request', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)
    const p1 = controller.enable(en0Pair)
    const p2 = controller.enable(en0Pair)
    expect(channel.sent).toHaveLength(1) // only one request dispatched
    channel.emitLine(JSON.stringify({ id: 'req-1', ok: true, origin: 'https://x:1', fingerprint_sha256: 'aa', phrase: 'P' }))
    const [r1, r2] = await Promise.all([p1, p2])
    expect(r1).toEqual(r2)
  })

  it('allows a fresh enable after the previous one settles', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)
    const p1 = controller.enable(en0Pair)
    channel.emitLine(JSON.stringify({ id: 'req-1', ok: true, origin: 'https://x:1', fingerprint_sha256: 'aa', phrase: 'P' }))
    await p1
    const p2 = controller.enable(en0Pair)
    expect(channel.sent).toHaveLength(2)
    channel.emitLine(JSON.stringify({ id: 'req-2', ok: true, origin: 'https://x:1', fingerprint_sha256: 'aa', phrase: 'P' }))
    await p2
  })
})

describe('IOSAccessController.disable', () => {
  it('sends a disable request and resolves', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)
    const promise = controller.disable()
    expect(channel.lastRequest()).toEqual({ id: 'req-1', action: 'disable' })
    channel.emitLine(JSON.stringify({ id: 'req-1', ok: true }))
    await expect(promise).resolves.toBeUndefined()
  })
})

describe('IOSAccessController.reset', () => {
  it('sends a well-formed reset request and resolves with a fresh pairing display', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)

    const promise = controller.reset(en0Pair)
    expect(channel.lastRequest()).toEqual({
      id: 'req-1',
      action: 'reset',
      pair: { InterfaceName: 'en0', Address: '192.168.1.20', Family: 'ipv4' },
    })

    channel.emitLine(
      JSON.stringify({
        id: 'req-1',
        ok: true,
        port: 54322,
        origin: 'https://192.168.1.20:54322',
        fingerprint_sha256: 'ef'.repeat(32),
        phrase: 'IJ-KL',
      }),
    )

    const result = await promise
    expect(result.origin).toBe('https://192.168.1.20:54322')
    expect(result.port).toBe(54322)
    expect(result.phrase).toBe('IJ-KL')
    expect(result.fingerprintHex).toBe('ef'.repeat(32))
  })

  it('rejects on an error response', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)
    const promise = controller.reset(en0Pair)
    channel.emitLine(JSON.stringify({ id: 'req-1', ok: false, error: 'candidate gone' }))
    await expect(promise).rejects.toThrow('candidate gone')
  })

  it('coalesces concurrent reset calls onto one in-flight request', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)
    const p1 = controller.reset(en0Pair)
    const p2 = controller.reset(en0Pair)
    expect(channel.sent).toHaveLength(1) // only one request dispatched
    channel.emitLine(JSON.stringify({ id: 'req-1', ok: true, origin: 'https://x:1', fingerprint_sha256: 'aa', phrase: 'P' }))
    const [r1, r2] = await Promise.all([p1, p2])
    expect(r1).toEqual(r2)
  })

  it('does not coalesce with a concurrent enable — reset and enable are tracked independently', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)
    const enablePromise = controller.enable(en0Pair)
    const resetPromise = controller.reset(en0Pair)
    expect(channel.sent).toHaveLength(2)
    expect(channel.sent.map((s) => (JSON.parse(s) as { action: string }).action)).toEqual(['enable', 'reset'])
    channel.emitLine(JSON.stringify({ id: 'req-1', ok: true, origin: 'https://x:1', fingerprint_sha256: 'aa', phrase: 'P' }))
    channel.emitLine(JSON.stringify({ id: 'req-2', ok: true, origin: 'https://x:2', fingerprint_sha256: 'bb', phrase: 'Q' }))
    await Promise.all([enablePromise, resetPromise])
  })
})

describe('IOSAccessController timeouts and channel close', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('times out a request that never receives a response', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel, { requestTimeoutMs: 1_000 })
    const promise = controller.enable(en0Pair)
    const assertion = expect(promise).rejects.toThrow('timed out')
    await vi.advanceTimersByTimeAsync(1_000)
    await assertion
  })

  it('rejects every pending request when the channel closes', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)
    const promise = controller.enable(en0Pair)
    channel.emitClose()
    await expect(promise).rejects.toThrow('closed')
  })

  it('rejects new requests immediately once closed', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)
    channel.emitClose()
    await expect(controller.enable(en0Pair)).rejects.toThrow('closed')
  })

  it('ignores a response with an unknown id (no pending request)', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)
    // Should not throw.
    channel.emitLine(JSON.stringify({ id: 'unknown', ok: true }))
    // A subsequent real request still works.
    const promise = controller.disable()
    channel.emitLine(JSON.stringify({ id: 'req-1', ok: true }))
    await expect(promise).resolves.toBeUndefined()
  })

  it('ignores a malformed response line without crashing', async () => {
    const channel = new FakeChannel()
    const controller = new IOSAccessController(channel)
    channel.emitLine('not json')
    const promise = controller.disable()
    channel.emitLine(JSON.stringify({ id: 'req-1', ok: true }))
    await expect(promise).resolves.toBeUndefined()
  })
})

describe('ProcessControlChannel', () => {
  it('frames newline-delimited lines from a real stream and writes with a trailing newline', () => {
    const stream = new PassThrough()
    const channel = new ProcessControlChannel(stream)
    const received: string[] = []
    channel.onLine((line) => received.push(line))

    stream.emit('data', Buffer.from('{"id":"a"}\n{"id":"b"}\n'))
    expect(received).toEqual(['{"id":"a"}', '{"id":"b"}'])

    let written = ''
    stream.write = vi.fn((chunk: string) => {
      written = chunk
      return true
    }) as unknown as typeof stream.write
    channel.send('{"id":"c"}')
    expect(written).toBe('{"id":"c"}\n')
  })

  it('reassembles a line split across multiple chunks', () => {
    const stream = new PassThrough()
    const channel = new ProcessControlChannel(stream)
    const received: string[] = []
    channel.onLine((line) => received.push(line))

    stream.emit('data', Buffer.from('{"id":"a"'))
    stream.emit('data', Buffer.from(',"ok":true}\n'))
    expect(received).toEqual(['{"id":"a","ok":true}'])
  })

  it('drops a single line that exceeds the cap instead of ever delivering it', () => {
    const stream = new PassThrough()
    const channel = new ProcessControlChannel(stream)
    const received: string[] = []
    channel.onLine((line) => received.push(line))

    const oversized = 'x'.repeat(MAX_CONTROL_LINE_BYTES + 10)
    stream.emit('data', Buffer.from(oversized + '\n' + '{"id":"ok"}\n'))
    expect(received).toEqual(['{"id":"ok"}'])
  })

  it('invokes onClose handlers on both close and end', () => {
    const stream = new PassThrough()
    const channel = new ProcessControlChannel(stream)
    let closeCount = 0
    channel.onClose(() => closeCount++)
    stream.emit('close')
    stream.emit('end')
    expect(closeCount).toBe(2)
  })
})
