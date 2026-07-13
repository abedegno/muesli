import type { TranscriptSegment } from '../../shared/types'

export function transcriptToPlainText(segments: TranscriptSegment[]): string {
  return segments
    .map((seg) => (seg.speaker ? `${seg.speaker}: ${seg.text}` : seg.text))
    .join('\n')
}
