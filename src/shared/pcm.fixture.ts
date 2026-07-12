import { PCM_FRAME_BYTES, PCM_FRAME_SAMPLES } from './pcm'

export const PCM_FIXTURE_SAMPLE_PATTERN = [0, 8192, -8192, 16384, -16384, 24575, -24575, 32767] as const
export const PCM_FIXTURE_FRAME_REPEAT = PCM_FRAME_SAMPLES / PCM_FIXTURE_SAMPLE_PATTERN.length

export const PCM_FIXTURE_INPUT_SAMPLES = Float32Array.from(
  { length: PCM_FRAME_SAMPLES },
  (_, i) => PCM_FIXTURE_SAMPLE_PATTERN[i % PCM_FIXTURE_SAMPLE_PATTERN.length] / 0x7fff,
)

export const PCM_FIXTURE_EXPECTED_FRAME = makeExpectedFrame()

function makeExpectedFrame(): Uint8Array {
  const expected = new Uint8Array(PCM_FRAME_BYTES)
  for (let repeat = 0; repeat < PCM_FIXTURE_FRAME_REPEAT; repeat++) {
    for (let i = 0; i < PCM_FIXTURE_SAMPLE_PATTERN.length; i++) {
      const sample = PCM_FIXTURE_SAMPLE_PATTERN[i]
      const offset = (repeat * PCM_FIXTURE_SAMPLE_PATTERN.length + i) * 2
      const signed = sample < 0 ? 0x10000 + sample : sample
      expected[offset] = signed & 0xff
      expected[offset + 1] = (signed >> 8) & 0xff
    }
  }
  return expected
}
