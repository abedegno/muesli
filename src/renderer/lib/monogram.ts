export const MONOGRAM_TONES = ['teal', 'blue', 'violet', 'amber', 'rose', 'slate'] as const
export type MonogramTone = (typeof MONOGRAM_TONES)[number]

interface Monogram {
  initial: string
  tone: MonogramTone
}

type MonogramInput = { id: string; title: string } | { id: string; label: string }

export interface MonogramColor {
  bg: string
  fg: string
}

// djb2 — small, fast, deterministic across sessions.
function hash(s: string): number {
  let h = 5381
  for (let i = 0; i < s.length; i++) h = ((h << 5) + h + s.charCodeAt(i)) >>> 0
  return h
}

export function monogramColor(seed: string): MonogramColor {
  const hashed = hash(seed)
  const hue = hashed % 360
  const useLightBackground = hashed % 2 === 0
  return {
    bg: useLightBackground ? `hsl(${hue} 65% 84%)` : `hsl(${hue} 45% 34%)`,
    fg: useLightBackground ? '#111827' : '#f9fafb',
  }
}

export function monogram(item: MonogramInput): Monogram {
  const text = 'label' in item ? item.label : item.title
  const m = text.match(/[A-Za-z0-9]/)
  const initial = m ? m[0].toUpperCase() : '•'
  const tone = MONOGRAM_TONES[hash(item.id) % MONOGRAM_TONES.length]
  return { initial, tone }
}
