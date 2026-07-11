// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { findMatches, useFindReplace } from './useFindReplace'

// ---------------------------------------------------------------------------
// findMatches — pure function
// ---------------------------------------------------------------------------
describe('findMatches', () => {
  it('returns empty array for empty query', () => {
    expect(findMatches('hello world', '')).toEqual([])
  })

  it('returns empty array when there is no match', () => {
    expect(findMatches('hello world', 'xyz')).toEqual([])
  })

  it('finds a single match', () => {
    expect(findMatches('hello world', 'world')).toEqual([{ from: 6, to: 11 }])
  })

  it('finds multiple matches', () => {
    expect(findMatches('abcabc', 'abc')).toEqual([
      { from: 0, to: 3 },
      { from: 3, to: 6 },
    ])
  })

  it('is case-insensitive', () => {
    expect(findMatches('Hello World', 'hello')).toEqual([{ from: 0, to: 5 }])
    expect(findMatches('Hello World', 'WORLD')).toEqual([{ from: 6, to: 11 }])
  })

  it('handles overlapping start positions correctly (non-overlapping matches)', () => {
    // "aa" in "aaa" should match at 0 and then skip to 2, which does not match
    expect(findMatches('aaa', 'aa')).toEqual([{ from: 0, to: 2 }])
  })

  it('returns empty when text is shorter than query', () => {
    expect(findMatches('hi', 'hello')).toEqual([])
  })
})

// ---------------------------------------------------------------------------
// useFindReplace hook
// ---------------------------------------------------------------------------
describe('useFindReplace', () => {
  it('starts with no matches and currentIndex -1 for empty query', () => {
    const { result } = renderHook(() => useFindReplace('hello world', ''))
    expect(result.current.matches).toEqual([])
    expect(result.current.currentIndex).toBe(-1)
  })

  it('starts at index 0 when there are matches', async () => {
    const { result } = renderHook(() => useFindReplace('hello hello', 'hello'))
    // Allow effects to run
    await act(async () => {})
    expect(result.current.matches).toHaveLength(2)
    expect(result.current.currentIndex).toBe(0)
  })

  it('next() advances and wraps from last to first', async () => {
    const { result } = renderHook(() => useFindReplace('aaa', 'a'))
    await act(async () => {})
    expect(result.current.matches).toHaveLength(3)
    expect(result.current.currentIndex).toBe(0)

    act(() => { result.current.next() })
    expect(result.current.currentIndex).toBe(1)

    act(() => { result.current.next() })
    expect(result.current.currentIndex).toBe(2)

    // Wrap around
    act(() => { result.current.next() })
    expect(result.current.currentIndex).toBe(0)
  })

  it('prev() goes back and wraps from first to last', async () => {
    const { result } = renderHook(() => useFindReplace('aaa', 'a'))
    await act(async () => {})
    expect(result.current.currentIndex).toBe(0)

    // Wrap from first → last
    act(() => { result.current.prev() })
    expect(result.current.currentIndex).toBe(2)

    act(() => { result.current.prev() })
    expect(result.current.currentIndex).toBe(1)
  })

  it('replaceOne replaces only the current match', async () => {
    const { result } = renderHook(() => useFindReplace('hello hello', 'hello'))
    await act(async () => {})
    expect(result.current.currentIndex).toBe(0)

    const newText = result.current.replaceOne('hi')
    expect(newText).toBe('hi hello')
  })

  it('replaceOne replaces the second match when currentIndex is 1', async () => {
    const { result } = renderHook(() => useFindReplace('hello hello', 'hello'))
    await act(async () => {})

    act(() => { result.current.next() })
    expect(result.current.currentIndex).toBe(1)

    const newText = result.current.replaceOne('hi')
    expect(newText).toBe('hello hi')
  })

  it('replaceAll replaces every occurrence', async () => {
    const { result } = renderHook(() => useFindReplace('hello hello hello', 'hello'))
    await act(async () => {})

    const newText = result.current.replaceAll('hi')
    expect(newText).toBe('hi hi hi')
  })

  it('replaceAll with different-length replacement preserves surrounding text', async () => {
    const { result } = renderHook(() => useFindReplace('cat and cat', 'cat'))
    await act(async () => {})

    const newText = result.current.replaceAll('kitten')
    expect(newText).toBe('kitten and kitten')
  })

  it('replaceAll returns original text when no matches', async () => {
    const { result } = renderHook(() => useFindReplace('hello world', 'xyz'))
    await act(async () => {})

    const newText = result.current.replaceAll('foo')
    expect(newText).toBe('hello world')
  })

  it('replaceOne is case-insensitive (preserves original casing in surrounding text)', async () => {
    const { result } = renderHook(() => useFindReplace('Hello World', 'hello'))
    await act(async () => {})
    expect(result.current.currentIndex).toBe(0)

    // replaceOne replaces the matched range in the original text
    const newText = result.current.replaceOne('hi')
    expect(newText).toBe('hi World')
  })

  it('currentIndex is -1 when there are no matches', async () => {
    const { result } = renderHook(() => useFindReplace('hello world', 'xyz'))
    await act(async () => {})
    expect(result.current.currentIndex).toBe(-1)
  })
})
