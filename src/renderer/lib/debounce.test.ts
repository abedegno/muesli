import { describe, it, expect, vi } from 'vitest'
import { debounce } from './debounce'

describe('debounce', () => {
  it('coalesces rapid calls and uses the latest args', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = debounce(fn, 500)
    d('a'); d('b'); d('c')
    expect(fn).not.toHaveBeenCalled()
    vi.advanceTimersByTime(500)
    expect(fn).toHaveBeenCalledTimes(1)
    expect(fn).toHaveBeenLastCalledWith('c')
    vi.useRealTimers()
  })
  it('flush invokes the pending call immediately', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = debounce(fn, 500)
    d('x')
    d.flush()
    expect(fn).toHaveBeenCalledTimes(1)
    expect(fn).toHaveBeenLastCalledWith('x')
    vi.useRealTimers()
  })
})
