import { describe, expect, it } from 'vitest'
import { segmentIndexAtTime } from './transcriptPlayback'

describe('segmentIndexAtTime', () => {
  const segments = [
    { start_ms: 0, end_ms: 1000 },
    { start_ms: 1000, end_ms: 2000 },
    { start_ms: 3000, end_ms: 4000 },
  ]

  it.each([
    { label: 'before first segment', timeMs: -1, want: -1 },
    { label: 'exact start boundary', timeMs: 1000, want: 1 },
    { label: 'exact end boundary is half-open and belongs to the next segment', timeMs: 2000, want: -1 },
    { label: 'gap between segments', timeMs: 2500, want: -1 },
    { label: 'after the last segment', timeMs: 5000, want: -1 },
  ])('$label', ({ timeMs, want }) => {
    expect(segmentIndexAtTime(segments, timeMs)).toBe(want)
  })

  it('returns -1 for an empty segment list', () => {
    expect(segmentIndexAtTime([], 0)).toBe(-1)
  })
})
