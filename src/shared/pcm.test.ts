import { describe, expect, it } from 'vitest'
import fixture from './pcm.fixture.json'
import { PCM_FRAME_BYTES, PCM_FRAME_SAMPLES, PcmFrameEncoder } from './pcm'

function clampToInt16(sample: number): number {
  const clamped = Math.max(-1, Math.min(1, sample))
  return Math.round(clamped * 0x7fff)
}

function buildInputSamples(): Float32Array {
  const samples = new Float32Array(fixture.samplePattern.length * fixture.patternRepeats)
  let offset = 0
  for (let i = 0; i < fixture.patternRepeats; i++) {
    for (const sample of fixture.samplePattern) {
      samples[offset++] = sample
    }
  }
  return samples
}

function encodeExpectedFrame(samples: ArrayLike<number>): Uint8Array {
  const bytes = new Uint8Array(samples.length * 2)
  const view = new DataView(bytes.buffer)
  for (let i = 0; i < samples.length; i++) {
    view.setInt16(i * 2, clampToInt16(Number(samples[i])), true)
  }
  return bytes
}

describe('PcmFrameEncoder', () => {
  it('downsamples 16 kHz mono input into exact 200 ms s16le frames', () => {
    const encoder = new PcmFrameEncoder({
      inputSampleRate: fixture.inputSampleRate,
      frameMs: fixture.frameMs,
    })
    const input = buildInputSamples()

    expect(input).toHaveLength(PCM_FRAME_SAMPLES * 2)

    const frames = encoder.push(input)

    expect(frames).toHaveLength(2)
    expect(frames[0]).toHaveLength(PCM_FRAME_BYTES)
    expect(frames[1]).toHaveLength(PCM_FRAME_BYTES)
    expect(frames[0]).toEqual(encodeExpectedFrame(input.slice(0, PCM_FRAME_SAMPLES)))
    expect(frames[1]).toEqual(encodeExpectedFrame(input.slice(PCM_FRAME_SAMPLES, PCM_FRAME_SAMPLES * 2)))
    expect(encoder.flush()).toEqual([])
  })
})
