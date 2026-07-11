import { describe, it, expect } from 'vitest'
import {
  buildSubtitleCues,
  MIN_CUE_DURATION_MS,
  MAX_CUE_DURATION_MS,
  MAX_CHARS_PER_CUE,
} from './subtitleCues'
import type { TranscriptSegment } from '../../shared/types'

function seg(overrides: Partial<TranscriptSegment>): TranscriptSegment {
  return { start_ms: 0, end_ms: 1000, text: 'hello', source: 'mixed', ...overrides }
}

// Every produced cue must have a positive duration and non-empty text.
function assertWellFormed(cues: ReturnType<typeof buildSubtitleCues>) {
  for (const cue of cues) {
    expect(cue.endMs).toBeGreaterThan(cue.startMs)
    expect(cue.text.trim().length).toBeGreaterThan(0)
    expect(cue.endMs - cue.startMs).toBeGreaterThanOrEqual(MIN_CUE_DURATION_MS)
    expect(cue.endMs - cue.startMs).toBeLessThanOrEqual(MAX_CUE_DURATION_MS)
  }
}

// Weaker sibling of assertWellFormed for short blocks forced into many chunks
// (e.g. by an unbreakable long token), where enforcing MIN_CUE_DURATION_MS on
// every chunk would necessarily overshoot the block's real end -- the min
// floor is a readability nicety, not a hard requirement, and gets skipped in
// that case. Still requires every cue to be non-degenerate.
function assertNonDegenerate(cues: ReturnType<typeof buildSubtitleCues>) {
  for (const cue of cues) {
    expect(cue.endMs).toBeGreaterThan(cue.startMs)
    expect(cue.text.trim().length).toBeGreaterThan(0)
  }
}

describe('buildSubtitleCues', () => {
  it('returns no cues for an empty segment list', () => {
    expect(buildSubtitleCues([])).toEqual([])
  })

  it('handles segments with no speakers at all: merges adjacent segments, no prefix', () => {
    const segs = [
      seg({ start_ms: 0, end_ms: 1000, text: 'Hello there,' }),
      seg({ start_ms: 1000, end_ms: 2000, text: 'how are you?' }),
    ]
    const cues = buildSubtitleCues(segs)
    assertWellFormed(cues)
    expect(cues.length).toBe(1)
    expect(cues[0].speaker).toBeUndefined()
    expect(cues[0].text).not.toMatch(/^[^:]{1,20}:/) // no "Name:" style prefix
    expect(cues[0].text).toContain('Hello there,')
    expect(cues[0].text).toContain('how are you?')
  })

  it('prefixes cue text with the speaker name when a segment has a speaker', () => {
    const cues = buildSubtitleCues([seg({ speaker: 'Alice', text: 'hello there' })])
    assertWellFormed(cues)
    expect(cues[0].speaker).toBe('Alice')
    expect(cues[0].text).toBe('Alice: hello there')
  })

  it('starts a new cue on speaker change', () => {
    const segs = [
      seg({ start_ms: 0, end_ms: 1000, text: 'hi', speaker: 'Alice' }),
      seg({ start_ms: 1000, end_ms: 2000, text: 'hey', speaker: 'Bob' }),
    ]
    const cues = buildSubtitleCues(segs)
    assertWellFormed(cues)
    expect(cues.length).toBe(2)
    expect(cues[0].speaker).toBe('Alice')
    expect(cues[1].speaker).toBe('Bob')
  })

  it('does not split when speaker value is unchanged null/undefined (same "no speaker" bucket)', () => {
    const segs = [
      seg({ start_ms: 0, end_ms: 500, text: 'a', speaker: null }),
      seg({ start_ms: 500, end_ms: 1000, text: 'b', speaker: undefined }),
    ]
    const cues = buildSubtitleCues(segs)
    assertWellFormed(cues)
    expect(cues.length).toBe(1)
  })

  it('starts a new cue when the gap between segments exceeds ~800ms, even for the same speaker', () => {
    const segs = [
      seg({ start_ms: 0, end_ms: 1000, text: 'first thought', speaker: 'Alice' }),
      seg({ start_ms: 5000, end_ms: 6000, text: 'second thought', speaker: 'Alice' }),
    ]
    const cues = buildSubtitleCues(segs)
    assertWellFormed(cues)
    expect(cues.length).toBe(2)
  })

  it('does not split when the gap is small (<=800ms) for the same speaker', () => {
    const segs = [
      seg({ start_ms: 0, end_ms: 1000, text: 'first', speaker: 'Alice' }),
      seg({ start_ms: 1500, end_ms: 2500, text: 'second', speaker: 'Alice' }),
    ]
    const cues = buildSubtitleCues(segs)
    expect(cues.length).toBe(1)
  })

  it('skips segments with empty/whitespace-only text without producing an empty cue', () => {
    const segs = [
      seg({ start_ms: 0, end_ms: 1000, text: '   ', speaker: 'Alice' }),
      seg({ start_ms: 1000, end_ms: 2000, text: 'actual words', speaker: 'Alice' }),
    ]
    const cues = buildSubtitleCues(segs)
    assertWellFormed(cues)
    expect(cues.length).toBe(1)
    expect(cues[0].text).toContain('actual words')
  })

  it('handles a single very long segment: long duration and long text, splitting both by time and by char count', () => {
    const longText = Array.from({ length: 40 }, (_, i) => `word${i}`).join(' ') // ~280 chars
    const segs = [seg({ start_ms: 0, end_ms: 120_000, text: longText, speaker: 'Alice' })]
    const cues = buildSubtitleCues(segs)
    assertWellFormed(cues)
    // A single long segment must be split into multiple cues on both axes.
    expect(cues.length).toBeGreaterThan(1)
    for (const cue of cues) expect(cue.text.length).toBeLessThanOrEqual(MAX_CHARS_PER_CUE)
    // Cues should be in non-decreasing time order and not overlap.
    for (let i = 1; i < cues.length; i++) {
      expect(cues[i].startMs).toBeGreaterThanOrEqual(cues[i - 1].endMs)
    }
    // Reconstructing the words (minus the one-time speaker prefix) recovers the source text.
    const rebuilt = cues
      .map((c) => c.text)
      .join(' ')
      .replace(/^Alice: /, '')
    expect(rebuilt.split(/\s+/).filter(Boolean).length).toBe(40)
  })

  it('handles rapid speaker alternation without degenerate cues', () => {
    const segs: TranscriptSegment[] = []
    const speakers = ['Alice', 'Bob']
    for (let i = 0; i < 20; i++) {
      segs.push(
        seg({
          start_ms: i * 300,
          end_ms: i * 300 + 200,
          text: `bit ${i}`,
          speaker: speakers[i % 2],
        }),
      )
    }
    const cues = buildSubtitleCues(segs)
    assertWellFormed(cues)
    // Every segment alternates speaker, so each becomes its own block/cue.
    expect(cues.length).toBe(20)
    for (let i = 0; i < cues.length; i++) {
      expect(cues[i].speaker).toBe(speakers[i % 2])
    }
  })

  it('handles a segment with zero/negative source duration without producing a zero-duration cue', () => {
    const cues = buildSubtitleCues([seg({ start_ms: 1000, end_ms: 1000, text: 'blip' })])
    assertWellFormed(cues)
    expect(cues.length).toBe(1)
  })

  it('does not drop the tail of a segment whose duration is much larger than MAX_CUE_DURATION_MS', () => {
    // Short text, but a duration many times MAX_CUE_DURATION_MS -- previously,
    // clamping this single chunk's duration to MAX_CUE_DURATION_MS silently
    // truncated the cue stream well before end_ms.
    const endMs = MAX_CUE_DURATION_MS * 5 + 1234
    const segs = [seg({ start_ms: 0, end_ms: endMs, text: 'hello there, how are you today', speaker: 'Alice' })]
    const cues = buildSubtitleCues(segs)
    assertWellFormed(cues)
    expect(cues.length).toBeGreaterThan(1)
    expect(cues[0].startMs).toBe(0)
    // The last cue must reach (or land within a tiny epsilon of) the source's real end --
    // no dropped tail time.
    expect(cues[cues.length - 1].endMs).toBeGreaterThanOrEqual(endMs - 5)
    expect(cues[cues.length - 1].endMs).toBeLessThanOrEqual(endMs + 5)
    // Cues should tile the full range contiguously.
    for (let i = 1; i < cues.length; i++) expect(cues[i].startMs).toBe(cues[i - 1].endMs)
  })

  it('hard-slices an unbreakable long token (or long speaker name) so no cue text exceeds the hard char cap', () => {
    const longToken = 'x'.repeat(300) // one word, no whitespace to break on
    const segs = [seg({ start_ms: 0, end_ms: 2000, text: longToken, speaker: 'Alice' })]
    const cues = buildSubtitleCues(segs)
    assertNonDegenerate(cues)
    expect(cues.length).toBeGreaterThan(1)
    for (const cue of cues) expect(cue.text.length).toBeLessThanOrEqual(MAX_CHARS_PER_CUE)

    // A long, space-free speaker name should also never blow the cap.
    const longSpeaker = 'Speaker' + 'Z'.repeat(200)
    const cues2 = buildSubtitleCues([seg({ start_ms: 0, end_ms: 1000, text: 'hi', speaker: longSpeaker })])
    assertNonDegenerate(cues2)
    for (const cue of cues2) expect(cue.text.length).toBeLessThanOrEqual(MAX_CHARS_PER_CUE)
  })

  it('does not overshoot a SHORT segment forced into many chunks by an unbreakable long token', () => {
    // Round-2 review repro: a 2000ms segment containing a 300-char unbreakable
    // token forces several chunks; flooring every one of them to
    // MIN_CUE_DURATION_MS would previously stretch the total timeline to
    // 4000-5000ms+, well past the segment's real 2000ms end.
    const endMs = 2000
    const longToken = 'x'.repeat(300)
    const segs = [seg({ start_ms: 0, end_ms: endMs, text: longToken, speaker: 'Alice' })]
    const cues = buildSubtitleCues(segs)
    assertNonDegenerate(cues)
    expect(cues.length).toBeGreaterThan(1)

    expect(cues[0].startMs).toBe(0)
    // No overshoot: the last cue must land at (or within a tiny epsilon of)
    // the segment's real end -- not stretched past it by the min-duration floor.
    expect(cues[cues.length - 1].endMs).toBeGreaterThanOrEqual(endMs - 5)
    expect(cues[cues.length - 1].endMs).toBeLessThanOrEqual(endMs + 5)
    // Cues tile the block contiguously: no gaps, no overlaps.
    for (let i = 1; i < cues.length; i++) expect(cues[i].startMs).toBe(cues[i - 1].endMs)
  })
})
