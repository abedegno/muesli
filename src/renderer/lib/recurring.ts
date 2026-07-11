import type { Note, SmartList, RuleNode } from '../../shared/types'
import { isRuleGroup } from '../../shared/types'

const MIN_CLUSTER = 3

/**
 * Normalise a note title to a bare stem for clustering.
 *
 * Strips (in order):
 *  1. Trailing month-name tokens: " Jun 13", " Jun 2026", " January 2026"
 *  2. Trailing numeric / hash / paren tokens: " 2026-06-20", " (3)", " #14", " 3"
 */
export function normalizeTitle(title: string): string {
  let s = title.toLowerCase().trim()
  // Strip trailing month-name + optional day / year
  s = s.replace(/\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s*\d{0,4}$/, '')
  // Strip trailing numeric / hash / paren / ISO-date tokens
  s = s.replace(/\s*[#(]?\d[\d/:.\-]*\)?$/g, '')
  return s.trim()
}

export interface RecurringSuggestion {
  stem: string
  count: number
}

/**
 * Walk all title-contains leaf conditions in an existing SmartList's rule tree
 * and collect the lowercased values.
 */
function collectTitleContainsValues(node: RuleNode): string[] {
  if (isRuleGroup(node)) {
    return node.children.flatMap(collectTitleContainsValues)
  }
  if (node.field === 'title' && node.operator === 'contains') {
    return [String(node.value).toLowerCase()]
  }
  return []
}

/**
 * Suggest recurring-meeting stems from a list of notes.
 *
 * A stem is suggested when:
 *  - it appears on >= MIN_CLUSTER (3) notes, AND
 *  - no existing SmartList already has a `title contains <value>` rule whose
 *    value (case-insensitive) is a substring of or matches the stem.
 */
export function suggestRecurring(notes: Note[], existing: SmartList[]): RecurringSuggestion[] {
  // Count occurrences of each normalised stem
  const counts = new Map<string, number>()
  for (const n of notes) {
    const stem = normalizeTitle(n.title)
    if (stem) {
      counts.set(stem, (counts.get(stem) ?? 0) + 1)
    }
  }

  // Build a set of lowercased values already covered by existing smart-list rules
  const coveredValues = existing.flatMap(sl => collectTitleContainsValues(sl.rule))

  // Keep only stems that meet the threshold and aren't already covered
  const suggestions: RecurringSuggestion[] = []
  for (const [stem, count] of counts) {
    if (count < MIN_CLUSTER) continue
    const alreadyCovered = coveredValues.some(v => v.includes(stem))
    if (alreadyCovered) continue
    suggestions.push({ stem, count })
  }

  // Return deterministically sorted by count desc, then stem asc
  suggestions.sort((a, b) => b.count - a.count || a.stem.localeCompare(b.stem))
  return suggestions
}
