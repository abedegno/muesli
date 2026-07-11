import type { FullNote, NoteStatus } from '../../shared/types'
import { isTerminal } from '../../shared/types'

interface PollOptions {
  intervalMs?: number
  maxAttempts?: number
  signal?: AbortSignal
  onUpdate?: (full: FullNote) => void
}

const sleep = (ms: number, signal?: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    if (signal?.aborted) return reject(new Error('aborted'))
    const t = setTimeout(resolve, ms)
    signal?.addEventListener(
      'abort',
      () => {
        clearTimeout(t)
        reject(new Error('aborted'))
      },
      { once: true },
    )
  })

// pollNote repeatedly fetches the full note until its status is terminal
// (ready|failed). It reports every poll via onUpdate and supports cancellation
// (AbortSignal) and a maxAttempts ceiling. Pure logic — getFull is injected.
export async function pollNote(
  id: string,
  getFull: (id: string) => Promise<FullNote>,
  opts: PollOptions = {},
): Promise<FullNote> {
  const { intervalMs = 3000, maxAttempts = Infinity, signal, onUpdate } = opts
  let attempts = 0
  while (true) {
    if (signal?.aborted) throw new Error('aborted')
    const full = await getFull(id)
    attempts++
    onUpdate?.(full)
    if (isTerminal(full.note.status as NoteStatus)) return full
    if (attempts >= maxAttempts) throw new Error('gave up waiting for note to be ready')
    await sleep(intervalMs, signal)
  }
}
