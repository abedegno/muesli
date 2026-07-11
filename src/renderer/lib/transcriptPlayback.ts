import type { TranscriptSegment } from '../../shared/types'

// Returns the 0-based segment index containing timeMs in a half-open interval.
// `[start_ms, end_ms)` means an exact end boundary belongs to the next segment,
// or -1 if no later segment starts there.
export function segmentIndexAtTime(
  segments: Pick<TranscriptSegment, 'start_ms' | 'end_ms'>[],
  timeMs: number,
): number {
  for (let i = 0; i < segments.length; i++) {
    const seg = segments[i]
    if (timeMs >= seg.start_ms && timeMs < seg.end_ms) return i
  }
  return -1
}
