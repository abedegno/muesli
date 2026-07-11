import { describe, it, expect } from 'vitest'
import { cuesToSrt, cuesToAss, cuesToVtt } from './subtitleFormats'
import type { SubtitleCue } from './subtitleCues'

const cues: SubtitleCue[] = [
  { startMs: 0, endMs: 1500, speaker: 'Alice', text: 'Alice: hello there' },
  { startMs: 1500, endMs: 3661_040, text: 'a long pause later' }, // 1h 1m 1s 40ms, no speaker
]

describe('cuesToSrt', () => {
  it('returns an empty string for no cues', () => {
    expect(cuesToSrt([])).toBe('')
  })

  it('renders numbered blocks with HH:MM:SS,mmm timestamps, blank-line separated', () => {
    const srt = cuesToSrt(cues)
    const blocks = srt.trim().split('\n\n')
    expect(blocks).toHaveLength(2)

    expect(blocks[0]).toBe('1\n00:00:00,000 --> 00:00:01,500\nAlice: hello there')
    expect(blocks[1]).toBe('2\n00:00:01,500 --> 01:01:01,040\na long pause later')
  })

  it('ends with a trailing newline', () => {
    expect(cuesToSrt(cues.slice(0, 1)).endsWith('\n')).toBe(true)
  })
})

describe('cuesToVtt', () => {
  it('returns a WebVTT header and blank output for no cues', () => {
    expect(cuesToVtt([])).toBe('WEBVTT\n\n')
  })

  it('renders cues with dot-millisecond timestamps and preserves cue text', () => {
    const vtt = cuesToVtt(cues)
    const blocks = vtt.trim().split('\n\n')
    expect(blocks).toHaveLength(3)
    expect(blocks[0]).toBe('WEBVTT')
    expect(blocks[1]).toBe('00:00:00.000 --> 00:00:01.500\nAlice: hello there')
    expect(blocks[2]).toBe('00:00:01.500 --> 01:01:01.040\na long pause later')
  })

  it('ends with a trailing newline', () => {
    expect(cuesToVtt(cues.slice(0, 1)).endsWith('\n')).toBe(true)
  })
})

describe('cuesToAss', () => {
  it('includes the required ASS sections and a default style', () => {
    const ass = cuesToAss(cues)
    expect(ass).toContain('[Script Info]')
    expect(ass).toContain('[V4+ Styles]')
    expect(ass).toContain('Style: Default,')
    expect(ass).toContain('[Events]')
    expect(ass).toContain('Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text')
  })

  it('renders one Dialogue line per cue with H:MM:SS.cc timestamps', () => {
    const ass = cuesToAss(cues)
    const dialogueLines = ass.split('\n').filter((l) => l.startsWith('Dialogue:'))
    expect(dialogueLines).toHaveLength(2)
    expect(dialogueLines[0]).toBe('Dialogue: 0,0:00:00.00,0:00:01.50,Default,,0,0,0,,Alice: hello there')
    expect(dialogueLines[1]).toBe('Dialogue: 0,0:00:01.50,1:01:01.04,Default,,0,0,0,,a long pause later')
  })

  it('produces no Dialogue lines for an empty cue list, but keeps the header', () => {
    const ass = cuesToAss([])
    expect(ass).toContain('[Events]')
    expect(ass.split('\n').filter((l) => l.startsWith('Dialogue:'))).toHaveLength(0)
  })
})
