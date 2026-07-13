import { useEffect, useMemo, useRef, useState } from 'react'
import { Search } from 'lucide-react'
import type { CSSProperties } from 'react'
import { cn } from '@/lib/cn'
import { highlightSearchText } from '@/lib/searchHighlight'
import type { TranscriptSegment } from '../../shared/types'

function ts(ms: number): string {
  const s = Math.floor(ms / 1000)
  return `${Math.floor(s / 60)}:${(s % 60).toString().padStart(2, '0')}`
}

// Golden-angle colour generation — gives maximally perceptually distinct hues for any
// number of speakers with zero collision risk. Index 0 → 0°, 1 → 137.5°, 2 → 275°, …
// The golden angle (≈137.508°) is irrational, so successive hues never repeat.
const GOLDEN_ANGLE_DEG = 137.508

function speakerStyle(idx: number): CSSProperties {
  const hue = (idx * GOLDEN_ANGLE_DEG) % 360
  return {
    color: `hsl(${hue.toFixed(1)}, 55%, 40%)`,
    borderColor: `hsl(${hue.toFixed(1)}, 55%, 60%)`,
  }
}

type FilteredItem = { seg: TranscriptSegment; idx: number }
type SpeakerGroup = { speaker: string | null; items: FilteredItem[] }

function groupBySpeaker(filtered: FilteredItem[]): SpeakerGroup[] {
  const groups: SpeakerGroup[] = []
  for (const item of filtered) {
    const speaker = (item.seg.speaker != null && item.seg.speaker !== '') ? item.seg.speaker : null
    const last = groups[groups.length - 1]
    if (!last || last.speaker !== speaker) {
      groups.push({ speaker, items: [item] })
    } else {
      last.items.push(item)
    }
  }
  return groups
}

export function TranscriptView({
  segments,
  highlightIndex,
  onSeek,
  playingIndex,
  speakerAliases,
  searchQuery,
  searchCurrentIndex,
  searchStartIndex = 0,
  hideSearchInput = false,
}: {
  segments: TranscriptSegment[]
  /** 0-based index of a segment to scroll to and highlight (e.g. a summary citation). */
  highlightIndex?: number | null
  /** Called when a segment line is clicked; makes the line keyboard/click interactive. */
  onSeek?: (startMs: number) => void
  /** 0-based index of the segment currently under the audio playhead. */
  playingIndex?: number | null
  /**
   * Optional alias map — e.g. { 'Speaker 1': 'Alice' }.
   * Resolved as speakerAliases?.[speaker] ?? speaker before rendering headings.
   * Pass this from DZ03b to rename speakers without restructuring the component.
   */
  speakerAliases?: Record<string, string>
  /** Optional note-level search query; when present, the component highlights but does not filter. */
  searchQuery?: string
  /** Global 0-based index of the current search match. */
  searchCurrentIndex?: number | null
  /** Global 0-based index to start counting matches from for this transcript block. */
  searchStartIndex?: number
  /** Hide the standalone transcript search box when a note-level search widget is handling find. */
  hideSearchInput?: boolean
}) {
  const [q, setQ] = useState('')
  const externalSearch = searchQuery != null
  const effectiveQuery = (searchQuery ?? q).trim()

  const filtered = useMemo(() => {
    const needle = effectiveQuery.toLowerCase()
    return segments
      .map((seg, idx) => ({ seg, idx }))
      .filter(({ seg }) => (externalSearch ? true : (needle ? seg.text.toLowerCase().includes(needle) : true)))
  }, [effectiveQuery, externalSearch, segments])

  // Whether diarization is on: any segment in the FULL array has a non-empty speaker.
  const hasSpeakers = useMemo(
    () => segments.some(s => s.speaker != null && s.speaker !== ''),
    [segments],
  )

  // Assign each new speaker a sequential 0-based index by order of first appearance.
  // The index is fed into speakerStyle() which uses the golden angle to derive a hue —
  // guaranteeing perceptually distinct colours for any realistic number of speakers.
  const speakerIndexMap = useMemo(() => {
    const map: Record<string, number> = {}
    let count = 0
    for (const seg of segments) {
      if (seg.speaker && !(seg.speaker in map)) {
        map[seg.speaker] = count
        count++
      }
    }
    return map
  }, [segments])

  const citedRef = useRef<HTMLLIElement | null>(null)
  const pendingScroll = useRef(false)
  const hasHighlight =
    highlightIndex != null && highlightIndex >= 0 && highlightIndex < segments.length

  // New citation target requested: mark a pending scroll and drop any active filter so the
  // cited segment is back in the DOM. Deliberately does NOT depend on `q` so it does not
  // re-fire on plain filter typing (no spurious scrolls).
  useEffect(() => {
    if (!hasHighlight) return
    pendingScroll.current = true
    if (q.trim() !== '') setQ('')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasHighlight, highlightIndex])

  // Perform the scroll once the cited element is mounted (also after the filter-clear
  // re-renders `filtered`). jsdom doesn't implement scrollIntoView — guard for that.
  useEffect(() => {
    if (!hasHighlight || !pendingScroll.current) return
    const el = citedRef.current
    if (!el) return
    pendingScroll.current = false
    if (typeof el.scrollIntoView === 'function') {
      el.scrollIntoView({ block: 'center' })
    }
  }, [hasHighlight, highlightIndex, filtered])

  if (segments.length === 0) {
    return <p className="text-sm text-muted-foreground">No transcript yet.</p>
  }

  let searchCursor = searchStartIndex

  // Renders a list of individual segment items (shared by both paths).
  const renderItems = (items: FilteredItem[]) =>
    items.map(({ seg, idx }) => {
      const cited = hasHighlight && idx === highlightIndex
      const playing = playingIndex != null && idx === playingIndex
      const highlighted = highlightSearchText(seg.text, effectiveQuery, searchCursor, searchCurrentIndex ?? null)
      searchCursor += highlighted.matchCount
      const interactiveClass = cn(
        'flex w-full gap-3 rounded-[var(--radius)] text-sm text-left',
        onSeek && 'cursor-pointer',
        cited && 'bg-primary/10 ring-2 ring-primary',
        playing && 'bg-emerald-500/10 ring-1 ring-emerald-500/40',
      )
      const content = (
        <>
          <span className="shrink-0 font-mono tabular-nums text-muted-foreground">{ts(seg.start_ms)}</span>
          <span>{highlighted.nodes}</span>
        </>
      )
      return (
        <li
          key={idx}
          ref={cited ? citedRef : undefined}
          data-cited={cited ? 'true' : undefined}
          data-playing={playing ? 'true' : undefined}
        >
          {onSeek ? (
            <button
              type="button"
              onClick={() => onSeek(seg.start_ms)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  onSeek(seg.start_ms)
                }
              }}
              className={interactiveClass}
            >
              {content}
            </button>
          ) : (
            <div className={interactiveClass}>
              {content}
            </div>
          )}
        </li>
      )
    })

  return (
    <div className="flex flex-col gap-2">
      {!externalSearch && !hideSearchInput ? (
        <label className="relative block">
          <Search size={14} className="absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground" />
          <input
            aria-label="Search transcript"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search transcript…"
            className="h-8 w-full rounded-[var(--radius)] border border-input bg-background pl-7 pr-2 text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        </label>
      ) : null}
      {!externalSearch && filtered.length === 0 ? (
        <p className="text-sm text-muted-foreground">No matching lines.</p>
      ) : hasSpeakers ? (
        <div className="flex flex-col gap-4">
          {groupBySpeaker(filtered).map((group, gi) => {
            const rawSpeaker = group.speaker
            const accentIdx = speakerIndexMap[rawSpeaker ?? ''] ?? 0
            const displayName = rawSpeaker != null
              ? (speakerAliases?.[rawSpeaker] ?? rawSpeaker)
              : null
            return (
              <div key={gi}>
                {rawSpeaker != null && (
                  <div
                    className="mb-1 border-l-2 pl-2 text-xs font-semibold uppercase tracking-wide"
                    style={speakerStyle(accentIdx)}
                    data-speaker={rawSpeaker}
                    data-speaker-accent={accentIdx}
                  >
                    {displayName}
                  </div>
                )}
                <ul className="flex flex-col gap-2">
                  {renderItems(group.items)}
                </ul>
              </div>
            )
          })}
        </div>
      ) : (
        <ul className="flex flex-col gap-2">
          {renderItems(filtered)}
        </ul>
      )}
    </div>
  )
}
