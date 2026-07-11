import { describe, it, expect } from 'vitest'
import { monogram, MONOGRAM_TONES } from './monogram'

describe('monogram', () => {
  it('takes the first alphanumeric of the title, uppercased', () => {
    expect(monogram({ id: 'x', title: 'standup' }).initial).toBe('S')
    expect(monogram({ id: 'x', title: '  hi there' }).initial).toBe('H')
    expect(monogram({ id: 'x', title: '42 things' }).initial).toBe('4')
  })
  it('falls back to a bullet when there is no alphanumeric', () => {
    expect(monogram({ id: 'x', title: '...' }).initial).toBe('•')
    expect(monogram({ id: 'x', title: '' }).initial).toBe('•')
  })
  it('picks a tone from the palette, deterministically by id', () => {
    const a = monogram({ id: 'note-123', title: 'A' })
    const b = monogram({ id: 'note-123', title: 'Different title' })
    expect(MONOGRAM_TONES).toContain(a.tone)
    expect(a.tone).toBe(b.tone) // same id → same tone, regardless of title
  })
})
