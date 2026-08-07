/** Streaming transcript output sample rate, in Hz; main expects 16 kHz audio. */
export const PCM_TARGET_SAMPLE_RATE = 16_000
/** Duration of each complete streaming frame, in milliseconds. */
export const PCM_FRAME_MS = 200
/** Mono samples per complete frame at {@link PCM_TARGET_SAMPLE_RATE}. */
export const PCM_FRAME_SAMPLES = Math.round((PCM_TARGET_SAMPLE_RATE * PCM_FRAME_MS) / 1000)
/** Bytes per complete mono signed-16-bit frame; samples are little-endian. */
export const PCM_FRAME_BYTES = PCM_FRAME_SAMPLES * 2

/** Renderer-side encoder settings; sample rates are positive finite values in Hz. */
export interface PcmFrameEncoderOptions {
  inputSampleRate: number
  targetSampleRate?: number
  frameMs?: number
}

function clampToInt16(sample: number): number {
  const clamped = Math.max(-1, Math.min(1, sample))
  return Math.round(clamped * 0x7fff)
}

function encodeFrame(samples: Int16Array): Uint8Array {
  const bytes = new Uint8Array(samples.length * 2)
  const view = new DataView(bytes.buffer)
  for (let i = 0; i < samples.length; i++) {
    view.setInt16(i * 2, samples[i], true)
  }
  return bytes
}

/**
 * Deterministic mono PCM frame encoder for live transcript streaming.
 *
 * The renderer feeds mono, non-interleaved Float32 samples in the normalized
 * `[-1, 1]` range. The encoder emits 16 kHz signed-16-bit little-endian frames
 * in 200 ms slices for main's note-stream IPC channel. Complete frames contain
 * 3,200 samples/6,400 bytes; {@link PcmFrameEncoder.flush} may emit one shorter
 * final frame. Instances retain resampling state across `push` calls and are
 * live stream processors, not snapshot encoders.
 */
export class PcmFrameEncoder {
  private readonly inputSampleRate: number
  private readonly targetSampleRate: number
  private readonly inputStep: number
  private readonly frameSamples: number
  private readonly frameBuffer: Int16Array

  private input: number[] = []
  private nextInputPosition = 0
  private frameIndex = 0

  constructor(options: PcmFrameEncoderOptions) {
    if (!Number.isFinite(options.inputSampleRate) || options.inputSampleRate <= 0) {
      throw new Error('inputSampleRate must be a positive finite number')
    }
    this.inputSampleRate = options.inputSampleRate
    this.targetSampleRate = options.targetSampleRate ?? PCM_TARGET_SAMPLE_RATE
    this.inputStep = this.inputSampleRate / this.targetSampleRate
    this.frameSamples = Math.round(((options.frameMs ?? PCM_FRAME_MS) * this.targetSampleRate) / 1000)
    this.frameBuffer = new Int16Array(this.frameSamples)
  }

  push(samples: Float32Array | ArrayLike<number>): Uint8Array[] {
    for (let i = 0; i < samples.length; i++) {
      this.input.push(Number(samples[i]))
    }
    return this.drain()
  }

  flush(): Uint8Array[] {
    const out = this.drain()
    if (this.frameIndex > 0) {
      out.push(encodeFrame(this.frameBuffer.slice(0, this.frameIndex)))
      this.frameIndex = 0
    }
    return out
  }

  private drain(): Uint8Array[] {
    const frames: Uint8Array[] = []
    while (this.nextInputPosition <= this.input.length - 1) {
      const floor = Math.floor(this.nextInputPosition)
      const frac = this.nextInputPosition - floor
      const current = this.input[floor] ?? 0
      const next = this.input[floor + 1] ?? current
      const sample = frac === 0 ? current : current + (next - current) * frac

      this.frameBuffer[this.frameIndex++] = clampToInt16(sample)
      this.nextInputPosition += this.inputStep

      if (this.frameIndex === this.frameSamples) {
        frames.push(encodeFrame(this.frameBuffer))
        this.frameIndex = 0
      }
    }

    const keepFrom = Math.max(0, Math.floor(this.nextInputPosition) - 1)
    if (keepFrom > 0) {
      this.input = this.input.slice(keepFrom)
      this.nextInputPosition -= keepFrom
    }

    return frames
  }
}
