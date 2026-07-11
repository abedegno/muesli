import { describe, expect, it, vi } from 'vitest'
import type { FullNote, NoteStatus } from '../../shared/types'
import { pollNote } from './pollNote'

function fullWith(status: NoteStatus): FullNote {
  return {
    note: {
      id: 'n1',
      title: 'T',
      status,
      created_at: '',
      updated_at: '',
      partial_transcript: false,
    },
    body_markdown: '',
    // Mirror the real server: transcript is null until the note is ready.
    transcript:
      status === 'ready'
        ? { segments: [{ start_ms: 0, end_ms: 1000, text: 'hi', source: 'mixed' }] }
        : null,
    summaries: [],
  }
}

describe('pollNote', () => {
  it('tolerates transcript: null on intermediate (pre-ready) polls', async () => {
    const sequence: NoteStatus[] = ['transcribing', 'ready']
    let i = 0
    const getFull = vi.fn(async () => fullWith(sequence[Math.min(i++, sequence.length - 1)]))
    const seen: (FullNote['transcript'])[] = []

    const result = await pollNote('n1', getFull, {
      intervalMs: 0,
      onUpdate: (f) => seen.push(f.transcript),
    })

    // First poll (transcribing) must surface a null transcript without throwing;
    // only the terminal (ready) poll carries segments.
    expect(seen[0]).toBeNull()
    expect(result.transcript).not.toBeNull()
    expect(result.transcript?.segments).toHaveLength(1)
  })

  it('polls until status is ready, reporting intermediate states', async () => {
    const sequence: NoteStatus[] = ['transcribing', 'summarizing', 'ready']
    let i = 0
    const getFull = vi.fn(async () => fullWith(sequence[Math.min(i++, sequence.length - 1)]))
    const seen: NoteStatus[] = []

    const result = await pollNote('n1', getFull, {
      intervalMs: 0,
      onUpdate: (f) => seen.push(f.note.status),
    })

    expect(result.note.status).toBe('ready')
    expect(seen).toEqual(['transcribing', 'summarizing', 'ready'])
    expect(getFull).toHaveBeenCalledTimes(3)
  })

  it('stops on failed', async () => {
    const getFull = vi.fn(async () => fullWith('failed'))
    const result = await pollNote('n1', getFull, { intervalMs: 0 })
    expect(result.note.status).toBe('failed')
    expect(getFull).toHaveBeenCalledTimes(1)
  })

  it('aborts when the signal is already aborted', async () => {
    const getFull = vi.fn(async () => fullWith('transcribing'))
    const controller = new AbortController()
    controller.abort()
    await expect(
      pollNote('n1', getFull, { intervalMs: 0, signal: controller.signal }),
    ).rejects.toThrow(/abort/i)
  })

  it('gives up after maxAttempts', async () => {
    const getFull = vi.fn(async () => fullWith('transcribing'))
    await expect(
      pollNote('n1', getFull, { intervalMs: 0, maxAttempts: 3 }),
    ).rejects.toThrow(/gave up/i)
    expect(getFull).toHaveBeenCalledTimes(3)
  })
})
