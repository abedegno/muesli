export const PCM_TARGET_SAMPLE_RATE = 16_000
export const PCM_FRAME_MS = 200
export const PCM_FRAME_SAMPLES = Math.round((PCM_TARGET_SAMPLE_RATE * PCM_FRAME_MS) / 1000)
export const PCM_FRAME_BYTES = PCM_FRAME_SAMPLES * 2

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
 * The encoder accepts Float32 samples at the source sample rate and emits
 * 16 kHz s16le frames in 200 ms slices. It keeps enough state to bridge input
 * chunks cleanly across arbitrary boundaries.
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
