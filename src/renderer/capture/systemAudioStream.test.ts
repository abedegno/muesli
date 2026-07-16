// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { startSystemAudioStream, type SystemAudioDeps } from './systemAudioStream'

const flush = () => new Promise((r) => setTimeout(r, 0))

interface WriteInit {
  format: string
  sampleRate: number
  numberOfFrames: number
  numberOfChannels: number
  timestamp: number
  data: Float32Array
}

function makeGlobals() {
  const write = vi.fn().mockResolvedValue(undefined)
  const close = vi.fn().mockResolvedValue(undefined)
  const writer = { write, close, ready: Promise.resolve() }
  const TrackGenerator = vi.fn(function (this: unknown) {
    return { writable: { getWriter: () => writer } } as never
  }) as unknown as SystemAudioDeps['TrackGenerator']
  const AudioData = vi.fn((init: WriteInit) => init) as unknown as SystemAudioDeps['AudioData']
  const MediaStreamCtor = vi.fn((tracks: unknown[]) => ({ tracks })) as unknown as SystemAudioDeps['MediaStreamCtor']
  return { write, close, TrackGenerator, AudioData, MediaStreamCtor }
}

function makeBridge(fmt: { sampleRate: number; channels: number } | null) {
  let pcmCb: ((c: Uint8Array) => void) | null = null
  const unsubscribe = vi.fn()
  const deps = {
    systemAudioStart: vi.fn().mockResolvedValue(fmt),
    systemAudioStop: vi.fn().mockResolvedValue(undefined),
    onSystemAudioPcm: vi.fn((cb: (c: Uint8Array) => void) => {
      pcmCb = cb
      return unsubscribe
    }),
  }
  return { deps, unsubscribe, emit: (c: Uint8Array) => pcmCb?.(c) }
}

beforeEach(() => vi.clearAllMocks())

describe('startSystemAudioStream', () => {
  it('returns null when the MediaStreamTrackGenerator runtime API is missing', async () => {
    const { deps } = makeBridge({ sampleRate: 48000, channels: 2 })
    const handle = await startSystemAudioStream({ ...deps, TrackGenerator: undefined, AudioData: undefined, MediaStreamCtor: undefined })
    expect(handle).toBeNull()
    expect(deps.systemAudioStart).not.toHaveBeenCalled()
  })

  it('returns null and stops the helper when start yields no format', async () => {
    const g = makeGlobals()
    const { deps } = makeBridge(null)
    const handle = await startSystemAudioStream({ ...deps, ...g })
    expect(handle).toBeNull()
    expect(deps.systemAudioStop).toHaveBeenCalledTimes(1)
  })

  it('writes AudioData frames of the right shape once the jitter buffer fills', async () => {
    const g = makeGlobals()
    const { deps, emit } = makeBridge({ sampleRate: 48000, channels: 2 })
    const handle = await startSystemAudioStream({ ...deps, ...g, jitterMs: 0 })
    expect(handle).not.toBeNull()
    // 4 stereo frames = 4 * 2 * 4 bytes = 32 bytes
    emit(new Uint8Array(32))
    await flush()
    expect(g.write).toHaveBeenCalledTimes(1)
    const init = (g.AudioData as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0] as WriteInit
    expect(init).toMatchObject({ format: 'f32', sampleRate: 48000, numberOfFrames: 4, numberOfChannels: 2, timestamp: 0 })
    // second chunk advances the timestamp by 4 frames @ 48kHz = 83us
    emit(new Uint8Array(32))
    await flush()
    const init2 = (g.AudioData as unknown as ReturnType<typeof vi.fn>).mock.calls[1][0] as WriteInit
    expect(init2.timestamp).toBe(Math.round((4 / 48000) * 1_000_000))
  })

  it('reassembles whole frames across arbitrary chunk boundaries', async () => {
    const g = makeGlobals()
    const { deps, emit } = makeBridge({ sampleRate: 48000, channels: 2 })
    await startSystemAudioStream({ ...deps, ...g, jitterMs: 0 })
    emit(new Uint8Array(6)) // < one 8-byte frame -> buffered, no write
    await flush()
    expect(g.write).not.toHaveBeenCalled()
    emit(new Uint8Array(2)) // completes one frame
    await flush()
    expect(g.write).toHaveBeenCalledTimes(1)
    const init = (g.AudioData as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0] as WriteInit
    expect(init.numberOfFrames).toBe(1)
  })

  it('holds writes until the jitter buffer threshold is reached', async () => {
    const g = makeGlobals()
    const { deps, emit } = makeBridge({ sampleRate: 48000, channels: 2 })
    // jitter 10ms @ 48kHz = 480 frames
    await startSystemAudioStream({ ...deps, ...g, jitterMs: 10 })
    emit(new Uint8Array(100 * 8)) // 100 frames < 480 -> still priming
    await flush()
    expect(g.write).not.toHaveBeenCalled()
    emit(new Uint8Array(400 * 8)) // now 500 frames total >= 480 -> flush both
    await flush()
    expect(g.write).toHaveBeenCalledTimes(2)
  })

  it('stop() unsubscribes, closes the writer, and stops the helper', async () => {
    const g = makeGlobals()
    const { deps, unsubscribe } = makeBridge({ sampleRate: 48000, channels: 2 })
    const handle = await startSystemAudioStream({ ...deps, ...g })
    handle!.stop()
    await flush()
    expect(unsubscribe).toHaveBeenCalledTimes(1)
    expect(g.close).toHaveBeenCalledTimes(1)
    expect(deps.systemAudioStop).toHaveBeenCalledTimes(1)
    handle!.stop() // idempotent
    expect(g.close).toHaveBeenCalledTimes(1)
  })
})
