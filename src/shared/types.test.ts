import { describe, expect, it } from 'vitest'
import { isReady, isTerminal, NOTE_STATUSES } from './types'

describe('note status helpers', () => {
  it('exposes the server status progression', () => {
    expect(NOTE_STATUSES).toEqual([
      'recording',
      'uploaded',
      'transcribing',
      'summarizing',
      'ready',
      'failed',
    ])
  })

  it('isReady is true only for ready', () => {
    expect(isReady({ status: 'ready' } as any)).toBe(true)
    expect(isReady({ status: 'summarizing' } as any)).toBe(false)
  })

  it('isTerminal covers ready and failed', () => {
    expect(isTerminal('ready')).toBe(true)
    expect(isTerminal('failed')).toBe(true)
    expect(isTerminal('transcribing')).toBe(false)
  })
})
