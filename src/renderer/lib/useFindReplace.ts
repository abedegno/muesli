import { useState, useMemo, useEffect, useCallback } from 'react'

export interface MatchRange {
  from: number
  to: number
}

/**
 * Pure helper — exported so tests can call it directly without a React host.
 * Returns all case-insensitive occurrences of `query` inside `text`.
 */
export function findMatches(text: string, query: string): MatchRange[] {
  if (!query) return []
  const results: MatchRange[] = []
  const lowerText = text.toLowerCase()
  const lowerQuery = query.toLowerCase()
  let idx = 0
  while (idx <= lowerText.length - lowerQuery.length) {
    const found = lowerText.indexOf(lowerQuery, idx)
    if (found === -1) break
    results.push({ from: found, to: found + query.length })
    idx = found + query.length
  }
  return results
}

export interface FindReplaceResult {
  matches: MatchRange[]
  currentIndex: number
  next: () => void
  prev: () => void
  replaceOne: (replacement: string) => string
  replaceAll: (replacement: string) => string
}

/**
 * React hook that tracks find/replace state for a given text and query.
 * currentIndex starts at 0 when there are matches, resets when the query changes.
 */
export function useFindReplace(text: string, query: string): FindReplaceResult {
  const matches = useMemo(() => findMatches(text, query), [text, query])

  const [currentIndex, setCurrentIndex] = useState<number>(-1)

  // Reset to first match whenever the query changes
  useEffect(() => {
    setCurrentIndex(matches.length > 0 ? 0 : -1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query])

  // Clamp currentIndex when matches shrink (e.g. text changed after replace)
  useEffect(() => {
    setCurrentIndex((prev) => {
      if (matches.length === 0) return -1
      if (prev < 0) return 0
      if (prev >= matches.length) return matches.length - 1
      return prev
    })
  }, [matches.length])

  const next = useCallback(() => {
    if (matches.length === 0) return
    setCurrentIndex((i) => (i + 1) % matches.length)
  }, [matches.length])

  const prev = useCallback(() => {
    if (matches.length === 0) return
    setCurrentIndex((i) => (i - 1 + matches.length) % matches.length)
  }, [matches.length])

  const replaceOne = useCallback(
    (replacement: string): string => {
      if (currentIndex < 0 || currentIndex >= matches.length) return text
      const { from, to } = matches[currentIndex]
      return text.slice(0, from) + replacement + text.slice(to)
    },
    [text, matches, currentIndex],
  )

  const replaceAll = useCallback(
    (replacement: string): string => {
      if (matches.length === 0) return text
      // Replace from end to start so earlier offsets stay valid
      let result = text
      for (let i = matches.length - 1; i >= 0; i--) {
        const { from, to } = matches[i]
        result = result.slice(0, from) + replacement + result.slice(to)
      }
      return result
    },
    [text, matches],
  )

  return { matches, currentIndex, next, prev, replaceOne, replaceAll }
}
