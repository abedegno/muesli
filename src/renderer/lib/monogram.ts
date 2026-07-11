import type { Note } from '../../shared/types'

export const MONOGRAM_TONES = ['teal', 'blue', 'violet', 'amber', 'rose', 'slate'] as const
export type MonogramTone = (typeof MONOGRAM_TONES)[number]

interface Monogram {
  initial: string
  tone: MonogramTone
}

// djb2 — small, fast, deterministic across sessions.
function hash(s: string): number {
  let h = 5381
  for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) >>> 0
  return h
}

export function monogram(note: Pick<Note, 'id' | 'title'>): Monogram {
  const m = note.title.match(/[A-Za-z0-9]/)
  const initial = m ? m[0].toUpperCase() : '•'
  const tone = MONOGRAM_TONES[hash(note.id) % MONOGRAM_TONES.length]
  return { initial, tone }
}
