import { muesli } from '@/api'

// Turn the main-process system-audio PCM stream (raw interleaved little-endian
// float32, delivered chunk-by-chunk over `onSystemAudioPcm`) into a live
// MediaStream via a MediaStreamTrackGenerator, so electronCapture can route it
// to the right channel. Validated in Electron v42 (see the S0 spike note).

const DEFAULT_JITTER_MS = 80

export interface SystemAudioStreamHandle {
  stream: MediaStream
  stop: () => void
}

interface WriterLike {
  write: (chunk: unknown) => Promise<void>
  close: () => Promise<void>
  readonly ready: Promise<void>
}
interface TrackGeneratorLike extends MediaStreamTrack {
  readonly writable: { getWriter: () => WriterLike }
}
type TrackGeneratorCtor = new (init: { kind: 'audio' }) => TrackGeneratorLike
interface AudioDataInit {
  format: string
  sampleRate: number
  numberOfFrames: number
  numberOfChannels: number
  timestamp: number
  data: Float32Array
}
type AudioDataCtor = new (init: AudioDataInit) => unknown

export interface SystemAudioDeps {
  systemAudioStart: () => Promise<{ sampleRate: number; channels: number } | null>
  systemAudioStop: () => Promise<void>
  onSystemAudioPcm: (cb: (chunk: Uint8Array) => void) => () => void
  TrackGenerator?: TrackGeneratorCtor
  AudioData?: AudioDataCtor
  MediaStreamCtor?: typeof MediaStream
  jitterMs?: number
}

function defaultDeps(): SystemAudioDeps {
  const g = globalThis as unknown as {
    MediaStreamTrackGenerator?: TrackGeneratorCtor
    AudioData?: AudioDataCtor
    MediaStream?: typeof MediaStream
  }
  return {
    systemAudioStart: () => muesli.systemAudioStart(),
    systemAudioStop: () => muesli.systemAudioStop(),
    onSystemAudioPcm: (cb) => muesli.onSystemAudioPcm(cb),
    TrackGenerator: g.MediaStreamTrackGenerator,
    AudioData: g.AudioData,
    MediaStreamCtor: g.MediaStream,
  }
}

/**
 * Start capturing system audio as a MediaStream. Returns null when unsupported
 * (missing runtime APIs, non-macOS, helper unavailable) -> capture stays mic-only.
 * The caller must invoke `handle.stop()` when recording ends.
 */
export async function startSystemAudioStream(
  overrides: Partial<SystemAudioDeps> = {},
): Promise<SystemAudioStreamHandle | null> {
  const deps = { ...defaultDeps(), ...overrides }
  const { TrackGenerator, AudioData, MediaStreamCtor } = deps
  if (!TrackGenerator || !AudioData || !MediaStreamCtor) return null

  const fmt = await deps.systemAudioStart()
  if (!fmt || fmt.sampleRate < 1 || fmt.channels < 1) {
    await deps.systemAudioStop().catch(() => {})
    return null
  }
  const { sampleRate, channels } = fmt

  const generator = new TrackGenerator({ kind: 'audio' })
  const writer = generator.writable.getWriter()
  const stream = new MediaStreamCtor([generator])

  const jitterFrames = Math.floor(((deps.jitterMs ?? DEFAULT_JITTER_MS) / 1000) * sampleRate)
  const bytesPerFrame = 4 * channels

  let leftover = new Uint8Array(0)
  const pending: Float32Array[] = []
  let bufferedFrames = 0
  let priming = true
  let closed = false
  let timestampUs = 0
  let writeChain: Promise<void> = Promise.resolve()

  const writeSamples = async (samples: Float32Array) => {
    if (closed) return
    const numberOfFrames = Math.floor(samples.length / channels)
    if (numberOfFrames <= 0) return
    try {
      await writer.ready
      await writer.write(
        new AudioData({
          format: 'f32',
          sampleRate,
          numberOfFrames,
          numberOfChannels: channels,
          timestamp: timestampUs,
          data: samples,
        }),
      )
      timestampUs += Math.round((numberOfFrames / sampleRate) * 1_000_000)
    } catch {
      // writer closed or aborted -> drop
    }
  }

  const enqueue = (samples: Float32Array) => {
    writeChain = writeChain.then(() => writeSamples(samples))
  }

  const unsubscribe = deps.onSystemAudioPcm((chunk) => {
    if (closed || chunk.byteLength === 0) return
    // Reassemble whole frames across arbitrary IPC chunk boundaries.
    let combined: Uint8Array
    if (leftover.length === 0) {
      combined = chunk
    } else {
      combined = new Uint8Array(leftover.length + chunk.length)
      combined.set(leftover, 0)
      combined.set(chunk, leftover.length)
    }
    const usableFrames = Math.floor(combined.length / bytesPerFrame)
    const usableBytes = usableFrames * bytesPerFrame
    leftover = combined.slice(usableBytes)
    if (usableFrames === 0) return
    // Owned, 4-byte-aligned copy (the IPC buffer may be reused).
    const aligned = combined.slice(0, usableBytes)
    const samples = new Float32Array(aligned.buffer, 0, usableFrames * channels)
    pending.push(samples)
    bufferedFrames += usableFrames
    if (priming) {
      if (bufferedFrames < jitterFrames) return
      priming = false
    }
    while (pending.length > 0) enqueue(pending.shift() as Float32Array)
  })

  const stop = () => {
    if (closed) return
    closed = true
    unsubscribe()
    writeChain = writeChain.then(() => writer.close().catch(() => {}))
    void deps.systemAudioStop().catch(() => {})
  }

  return { stream, stop }
}
