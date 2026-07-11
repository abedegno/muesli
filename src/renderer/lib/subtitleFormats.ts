import type { SubtitleCue } from './subtitleCues'

function pad(n: number, width = 2): string {
  return Math.max(0, Math.trunc(n)).toString().padStart(width, '0')
}

// Splits a millisecond timestamp into its clock components, clamping negative
// input to zero so a malformed cue can't produce a malformed timestamp.
function splitMs(ms: number): { hours: number; minutes: number; seconds: number; millis: number } {
  const totalMs = Math.max(0, Math.round(ms))
  const hours = Math.floor(totalMs / 3_600_000)
  const minutes = Math.floor((totalMs % 3_600_000) / 60_000)
  const seconds = Math.floor((totalMs % 60_000) / 1_000)
  const millis = totalMs % 1_000
  return { hours, minutes, seconds, millis }
}

// SRT timestamps: HH:MM:SS,mmm (zero-padded, comma millis separator).
function toSrtTimestamp(ms: number): string {
  const { hours, minutes, seconds, millis } = splitMs(ms)
  return `${pad(hours)}:${pad(minutes)}:${pad(seconds)},${pad(millis, 3)}`
}

// ASS timestamps: H:MM:SS.cc (single-digit hours per the format's convention,
// centiseconds not milliseconds).
function toAssTimestamp(ms: number): string {
  const { hours, minutes, seconds, millis } = splitMs(ms)
  const centis = Math.floor(millis / 10)
  return `${hours}:${pad(minutes)}:${pad(seconds)}.${pad(centis)}`
}

// WebVTT timestamps: HH:MM:SS.mmm (same clock shape as SRT, dot millis).
function toVttTimestamp(ms: number): string {
  const { hours, minutes, seconds, millis } = splitMs(ms)
  return `${pad(hours)}:${pad(minutes)}:${pad(seconds)}.${pad(millis, 3)}`
}

// Renders cues as a standard numbered SRT file.
export function cuesToSrt(cues: SubtitleCue[]): string {
  if (cues.length === 0) return ''
  return (
    cues
      .map((cue, i) => `${i + 1}\n${toSrtTimestamp(cue.startMs)} --> ${toSrtTimestamp(cue.endMs)}\n${cue.text}`)
      .join('\n\n') + '\n'
  )
}

// Renders cues as a basic WebVTT file with the standard header.
export function cuesToVtt(cues: SubtitleCue[]): string {
  if (cues.length === 0) return 'WEBVTT\n\n'
  return (
    `WEBVTT\n\n` +
    cues
      .map((cue) => `${toVttTimestamp(cue.startMs)} --> ${toVttTimestamp(cue.endMs)}\n${cue.text}`)
      .join('\n\n') +
    '\n'
  )
}

// A minimal, readable ASS style: white text, subtle drop shadow, bottom-centered.
const ASS_HEADER = `[Script Info]
Title: Muesli transcript export
ScriptType: v4.00+
WrapStyle: 0
PlayResX: 1280
PlayResY: 720
ScaledBorderAndShadow: yes

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,42,&H00FFFFFF,&H000000FF,&H00000000,&H64000000,0,0,0,0,100,100,0,0,1,2,1,2,10,10,20,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`

// Renders cues as a basic ASS (Advanced SubStation Alpha) file using the
// single default style above.
export function cuesToAss(cues: SubtitleCue[]): string {
  const lines = cues.map(
    (cue) =>
      `Dialogue: 0,${toAssTimestamp(cue.startMs)},${toAssTimestamp(cue.endMs)},Default,,0,0,0,,${cue.text.replace(/\n/g, '\\N')}`,
  )
  return ASS_HEADER + lines.join('\n') + (lines.length ? '\n' : '')
}
