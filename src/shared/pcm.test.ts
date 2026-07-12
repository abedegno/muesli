import { describe, expect, it } from 'vitest'
import { PCM_FRAME_BYTES, PCM_FRAME_SAMPLES, PcmFrameEncoder } from './pcm'

function makeExpectedFrame(): Uint8Array {
  const expected = new Uint8Array(PCM_FRAME_BYTES)
  for (let i = 0; i < PCM_FRAME_SAMPLES; i++) {
    expected[i * 2] = 0x00
    expected[i * 2 + 1] = 0x20
  }
  return expected
}

describe('PcmFrameEncoder', () => {
  it('downsamples 48 kHz mono input into exact 200 ms 16 kHz s16le frames', () => {
    const encoder = new PcmFrameEncoder({ inputSampleRate: 48_000 })
    const input = new Float32Array(PCM_FRAME_SAMPLES * 6).fill(0.25)

    const frames = encoder.push(input)

    expect(frames).toHaveLength(2)
    expect(frames[0]).toHaveLength(PCM_FRAME_BYTES)
    expect(frames[1]).toHaveLength(PCM_FRAME_BYTES)
    expect(frames[0]).toEqual(makeExpectedFrame())
    expect(frames[1]).toEqual(makeExpectedFrame())
    expect(encoder.flush()).toEqual([])
  })
})
