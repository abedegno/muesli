import type { TranscriptSegment } from '../../shared/types'

// A subtitle cue: one on-screen caption with a time range and the text to show.
// `speaker` is metadata (the raw speaker name from the segment, if any); the
// speaker prefix is already folded into `text` (see buildSubtitleCues) so
// renderers can treat `text` as the literal caption to display.
export interface SubtitleCue {
  startMs: number
  endMs: number
  speaker?: string
  text: string
}

// Readability/timing constants. These are deliberately simple, well-known
// subtitling rules of thumb rather than derived from any spec:
//
// - TARGET_CHARS_PER_CUE (~42): the commonly cited "one comfortable line" length
//   for captions -- long enough to carry a thought, short enough to read in the
//   time a cue is typically on screen.
// - MAX_CHARS_PER_CUE (~84): about two lines' worth. This is a HARD cap: no cue's
//   rendered text may ever exceed it, even for a single unbreakable long token
//   (see `tokenize` below), so a caption is never a wall of text.
// - MIN_CUE_DURATION_MS (1000ms): don't flash a caption for under ~1s -- shorter
//   than that and it isn't readable.
// - MAX_CUE_DURATION_MS (7000ms): don't hold a caption for more than ~7s even if
//   the source segment/gap is very long -- keeps captions feeling responsive.
//   This is also authoritative on the *timing* side: rather than clamping an
//   overlong cue's duration (which would silently eat into the block's total
//   time and leave the tail of the source segment undisplayed), a cue whose
//   proportional share of the block's time would exceed this is split into
//   more, shorter cues up front -- see `expandForMaxDuration`.
// - MAX_GAP_MS (800ms): a silence longer than this reads as a new beat/turn in
//   the conversation, so we start a new cue even when the same speaker continues.
export const TARGET_CHARS_PER_CUE = 42
export const MAX_CHARS_PER_CUE = 84
export const MIN_CUE_DURATION_MS = 1000
export const MAX_CUE_DURATION_MS = 7000
export const MAX_GAP_MS = 800

// Normalizes speaker identity for comparison/grouping: null and undefined are
// the same "no speaker" bucket, distinct from any named speaker.
function speakerKey(speaker: string | null | undefined): string {
  return speaker == null ? ' __no_speaker__' : speaker
}

interface Block {
  startMs: number
  endMs: number
  speaker?: string | null
  parts: string[]
}

// Splits `text` into whitespace-delimited "atoms", additionally hard-slicing
// any atom longer than `maxLen` into `maxLen`-sized pieces. This guarantees no
// atom handed to the word-wrapper below can itself violate the hard char cap
// (e.g. a long URL/token, or a long speaker name with no spaces to break on).
function tokenize(text: string, maxLen: number): string[] {
  const atoms: string[] = []
  for (const word of text.split(/\s+/).filter(Boolean)) {
    if (word.length <= maxLen) {
      atoms.push(word)
    } else {
      for (let i = 0; i < word.length; i += maxLen) atoms.push(word.slice(i, i + maxLen))
    }
  }
  return atoms
}

// Greedily packs `atoms` into chunks: closes a chunk once it reaches `target`
// chars, and force-closes it if the next atom would push it past `max` chars.
// Since atoms are pre-sliced to `max` chars (see `tokenize`), no resulting
// chunk can exceed `max` chars either.
function packAtoms(atoms: string[], target: number, max: number): string[] {
  const chunks: string[] = []
  let current = ''
  for (const atom of atoms) {
    const candidate = current ? `${current} ${atom}` : atom
    if (current && (candidate.length > max || candidate.length > target)) {
      chunks.push(current)
      current = atom
    } else {
      current = candidate
    }
  }
  if (current) chunks.push(current)
  return chunks
}

// Word-wraps `text` into readable chunks, respecting a hard character cap
// even for a single unbreakable long token.
function splitText(text: string, target: number, max: number): string[] {
  return packAtoms(tokenize(text, max), target, max)
}

// Splits one chunk's text into `parts` roughly-equal-by-chars pieces (used to
// shrink a chunk whose proportional time share would exceed the max cue
// duration). Falls back to a hard character slice for a single unbreakable
// token/chunk with no word boundaries to split on.
function splitIntoParts(text: string, parts: number): string[] {
  if (parts <= 1) return [text]
  const words = text.split(/\s+/).filter(Boolean)
  if (words.length <= 1) {
    const size = Math.max(1, Math.ceil(text.length / parts))
    const pieces: string[] = []
    for (let i = 0; i < text.length; i += size) pieces.push(text.slice(i, i + size))
    return pieces
  }
  const totalChars = words.reduce((sum, w) => sum + w.length, 0) || 1
  const targetPerPart = totalChars / parts
  const pieces: string[] = []
  let current: string[] = []
  let currentChars = 0
  for (const word of words) {
    if (current.length && currentChars + word.length > targetPerPart && pieces.length < parts - 1) {
      pieces.push(current.join(' '))
      current = []
      currentChars = 0
    }
    current.push(word)
    currentChars += word.length
  }
  if (current.length) pieces.push(current.join(' '))
  return pieces
}

// Given readability-driven chunks and the FULL duration they must fit within,
// further subdivides any chunk whose proportional share of that duration
// would exceed MAX_CUE_DURATION_MS. This is what keeps the full
// `blockEndMs - blockStartMs` range authoritative: rather than clamping an
// overlong cue's duration during assignment (which would silently run out of
// time before reaching the block's real end), we make sure up front that
// every chunk's fair share already fits under the max, iterating to a fixed
// point since a split can occasionally still leave a share fractionally over
// (rounding) on a highly uneven distribution.
function expandForMaxDuration(chunks: string[], totalDurationMs: number): string[] {
  let current = chunks
  for (let iteration = 0; iteration < 20; iteration++) {
    const totalChars = current.reduce((sum, c) => sum + c.length, 0) || 1
    let changed = false
    const next: string[] = []
    for (const chunk of current) {
      const share = (chunk.length / totalChars) * totalDurationMs
      if (share > MAX_CUE_DURATION_MS) {
        next.push(...splitIntoParts(chunk, Math.ceil(share / MAX_CUE_DURATION_MS)))
        changed = true
      } else {
        next.push(chunk)
      }
    }
    current = next
    if (!changed) break
  }
  return current
}

// Splits one speaker-turn block into one or more timed cues: word-wraps the
// (speaker-prefixed) text, expands any chunk whose fair share of the block's
// total duration would exceed the max cue duration, then divides the FULL
// block time range across the final chunks proportionally by chunk length.
//
// The block's real start/end (`block.startMs`/`blockEndMs`) are always
// authoritative: the generated cues start at `block.startMs`, are
// contiguous, and the last one ends at exactly `blockEndMs`. The
// min-duration floor is a readability *nicety*, not a hard requirement --
// applying it can never be allowed to push the cumulative timing of a
// multi-cue block past `blockEndMs` (see the short-block regression test:
// a single unbreakable long token can force several chunks out of a very
// short segment, and flooring every one of them to MIN_CUE_DURATION_MS
// would overshoot the segment's real end many times over).
function splitBlockIntoCues(block: Block): SubtitleCue[] {
  const rawText = block.parts.join(' ').trim()
  if (!rawText) return []
  const prefixed = block.speaker ? `${block.speaker}: ${rawText}` : rawText

  const totalDurationMs = Math.max(block.endMs - block.startMs, 1)
  const blockEndMs = block.startMs + totalDurationMs

  const wrapped = splitText(prefixed, TARGET_CHARS_PER_CUE, MAX_CHARS_PER_CUE)
  if (wrapped.length === 0) return []
  const chunks = expandForMaxDuration(wrapped, totalDurationMs)

  if (chunks.length === 1) {
    // No splitting needed: a single cue for the whole block. There's no
    // cascading/tiling to protect here, so (as before) a very short/zero-length
    // source segment still gets a readable minimum on-screen time, even if
    // that means running slightly past the segment's real end.
    const duration = Math.min(Math.max(totalDurationMs, MIN_CUE_DURATION_MS), MAX_CUE_DURATION_MS)
    return [{ startMs: block.startMs, endMs: block.startMs + duration, speaker: block.speaker ?? undefined, text: chunks[0] }]
  }

  const totalChars = chunks.reduce((sum, c) => sum + c.length, 0) || 1
  // Natural (unfloored) proportional duration for every chunk but the last;
  // the last always takes whatever of `totalDurationMs` remains, so the
  // block's full range is exactly accounted for regardless of what happens
  // to the others. Every value is floored at 1ms so a chunk can never end up
  // with a zero/negative duration.
  const naturalNonLast = chunks
    .slice(0, -1)
    .map((c) => Math.max(1, Math.round(totalDurationMs * (c.length / totalChars))))

  // Only apply the MIN_CUE_DURATION_MS floor if doing so for every non-last
  // chunk still leaves the last chunk a non-negative share of the block's
  // time -- i.e. flooring must never push cumulative timing past blockEndMs.
  const flooredNonLast = naturalNonLast.map((d) => Math.min(Math.max(d, MIN_CUE_DURATION_MS), MAX_CUE_DURATION_MS))
  const flooredSum = flooredNonLast.reduce((sum, d) => sum + d, 0)
  const nonLastDurations =
    flooredSum < totalDurationMs ? flooredNonLast : naturalNonLast.map((d) => Math.min(d, MAX_CUE_DURATION_MS))

  const cues: SubtitleCue[] = []
  let cursor = block.startMs
  chunks.forEach((chunk, i) => {
    const isLast = i === chunks.length - 1
    // Never let the last chunk's duration go to zero/negative even in a
    // pathological input (e.g. far more forced chunks than the block's total
    // duration in ms) -- a 1ms floor here trades a tiny, unavoidable overshoot
    // for the hard "no degenerate cue" requirement.
    const duration = isLast ? Math.max(blockEndMs - cursor, 1) : nonLastDurations[i]
    const startMs = cursor
    const endMs = startMs + duration
    cues.push({ startMs, endMs, speaker: block.speaker ?? undefined, text: chunk })
    cursor = endMs
  })
  return cues
}

// Builds subtitle cues from an ordered list of transcript segments. Groups
// consecutive segments into "blocks" (same speaker, no large gap), then splits
// each block into readable, sensibly-timed cues. Handles: no segments, no
// speakers at all, a single very long segment (long duration and/or long
// text), and rapid speaker alternation -- always producing well-formed cues
// (start < end, non-empty text) without dropping any of the source's time range.
export function buildSubtitleCues(segments: TranscriptSegment[]): SubtitleCue[] {
  const blocks: Block[] = []
  for (const seg of segments) {
    const text = seg.text.trim()
    if (!text) continue // never produce an empty cue

    const prev = blocks[blocks.length - 1]
    const gap = prev ? seg.start_ms - prev.endMs : Infinity
    const speakerChanged = !prev || speakerKey(prev.speaker) !== speakerKey(seg.speaker)

    if (prev && !speakerChanged && gap <= MAX_GAP_MS) {
      prev.endMs = Math.max(prev.endMs, seg.end_ms)
      prev.parts.push(text)
    } else {
      blocks.push({ startMs: seg.start_ms, endMs: seg.end_ms, speaker: seg.speaker, parts: [text] })
    }
  }

  const cues: SubtitleCue[] = []
  for (const block of blocks) cues.push(...splitBlockIntoCues(block))
  return cues
}
