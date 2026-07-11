import type { FullNote } from '../../shared/types'

// fullNoteToMarkdown renders a complete note as Markdown: title, each summary panel,
// the user's own notes, and the transcript.
export function fullNoteToMarkdown(full: FullNote): string {
  const lines: string[] = [`# ${full.note.title || 'Untitled'}`, '']
  for (const s of full.summaries) {
    lines.push(`## ${s.template_name}`, '')
    for (const sec of s.sections) {
      lines.push(`### ${sec.heading}`, '', sec.content_markdown, '')
    }
  }
  if (full.body_markdown.trim()) {
    lines.push('## My notes', '', full.body_markdown, '')
  }
  const segs = full.transcript?.segments ?? []
  if (segs.length) {
    lines.push('## Transcript', '')
    const hasSpeaker = segs.some((seg) => !!seg.speaker)
    if (!hasSpeaker) {
      // No speaker attribution anywhere: preserve the original one-line-per-segment
      // rendering exactly (no grouping, no blank lines between segments).
      for (const seg of segs) lines.push(seg.text)
    } else {
      // Group consecutive segments that share the same (truthy) speaker into a
      // single block. Segments with no speaker never merge with a neighbour,
      // even another no-speaker segment, so they always render as their own block.
      const blocks: { speaker: string | null; texts: string[] }[] = []
      for (const seg of segs) {
        const speaker = seg.speaker || null
        const last = blocks[blocks.length - 1]
        if (speaker && last && last.speaker === speaker) {
          last.texts.push(seg.text)
        } else {
          blocks.push({ speaker, texts: [seg.text] })
        }
      }
      for (let i = 0; i < blocks.length; i++) {
        const block = blocks[i]
        const text = block.texts.join(' ')
        lines.push(block.speaker ? `**${block.speaker}:** ${text}` : text)
        if (i < blocks.length - 1) lines.push('')
      }
    }
    lines.push('')
  }
  return lines.join('\n')
}

/** Remove ATX heading markers from lines (e.g. `## Foo` -> `Foo`). */
function stripMarkdownHeadings(text: string): string {
  return text
    .split('\n')
    .map((line) => line.replace(/^#+\s*/, ''))
    .join('\n')
}

// fullNoteToPlainText renders a complete note as plain text: title, each summary
// section, the user's own notes, and the transcript (with speaker attribution,
// no timecodes). Designed so DZ01d (speaker export) can share this path cleanly.
export function fullNoteToPlainText(full: FullNote): string {
  const lines: string[] = [full.note.title || 'Untitled', '']

  for (const s of full.summaries) {
    for (const sec of s.sections) {
      lines.push(sec.heading, '')
      lines.push(stripMarkdownHeadings(sec.content_markdown), '')
    }
  }

  if (full.body_markdown.trim()) {
    lines.push('My notes', '')
    lines.push(stripMarkdownHeadings(full.body_markdown), '')
  }

  const segs = full.transcript?.segments ?? []
  if (segs.length) {
    lines.push('Transcript', '')
    for (let i = 0; i < segs.length; i++) {
      const seg = segs[i]
      const turn = seg.speaker ? `${seg.speaker}: ${seg.text}` : seg.text
      lines.push(turn)
      if (i < segs.length - 1) lines.push('') // blank line between segments
    }
    lines.push('')
  }

  return lines.join('\n')
}
