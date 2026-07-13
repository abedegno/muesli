import type { ReactNode } from 'react'
import { cn } from '@/lib/cn'

export function countCaseInsensitiveMatches(text: string, query: string): number {
  const needle = query.trim().toLowerCase()
  if (!needle) return 0

  const haystack = text.toLowerCase()
  let count = 0
  let index = 0
  while (index <= haystack.length - needle.length) {
    const hit = haystack.indexOf(needle, index)
    if (hit === -1) break
    count++
    index = hit + needle.length
  }
  return count
}

export interface HighlightedTextResult {
  nodes: ReactNode[]
  matchCount: number
}

/**
 * Wraps case-insensitive substring matches of `query` in <mark>.
 * The `currentMatchIndex` is a global 0-based index; the occurrence whose
 * index matches it gets a distinct current style and `data-note-search-current`.
 */
export function highlightSearchText(
  text: string,
  query: string,
  startMatchIndex: number,
  currentMatchIndex: number | null,
): HighlightedTextResult {
  const needle = query.trim().toLowerCase()
  if (!needle) return { nodes: [text], matchCount: 0 }

  const lower = text.toLowerCase()
  const nodes: ReactNode[] = []
  let index = 0
  let matchCount = 0

  while (index < text.length) {
    const hit = lower.indexOf(needle, index)
    if (hit === -1) {
      nodes.push(text.slice(index))
      break
    }

    if (hit > index) nodes.push(text.slice(index, hit))

    const globalMatchIndex = startMatchIndex + matchCount
    const isCurrent = currentMatchIndex != null && globalMatchIndex === currentMatchIndex
    nodes.push(
      <mark
        key={`${startMatchIndex}-${matchCount}-${hit}`}
        data-note-search-current={isCurrent ? 'true' : undefined}
        className={cn(
          'rounded-[0.2rem] px-0.5',
          isCurrent ? 'bg-primary text-primary-foreground ring-1 ring-primary' : 'bg-primary/20 text-foreground',
        )}
      >
        {text.slice(hit, hit + needle.length)}
      </mark>,
    )

    matchCount++
    index = hit + needle.length
  }

  if (nodes.length === 0) nodes.push(text)
  return { nodes, matchCount }
}
