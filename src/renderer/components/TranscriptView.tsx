import { useEffect, useMemo, useRef, useState } from 'react'
import { Search } from 'lucide-react'
import type { CSSProperties } from 'react'
import { cn } from '@/lib/cn'
import { countCaseInsensitiveMatches, highlightSearchText } from '@/lib/searchHighlight'
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
type TranscriptRow =
  | {
      kind: 'speaker'
      key: string
      speaker: string
      displayName: string
      accentIdx: number
      height: number
    }
  | {
      kind: 'segment'
      key: string
      idx: number
      seg: TranscriptSegment
      speaker: string | null
      height: number
    }

type SearchMeta = { start: number; matchCount: number }

const VIRTUALIZE_AFTER_SEGMENTS = 200
const SEGMENT_ROW_HEIGHT = 44
const SPEAKER_ROW_HEIGHT = 24
const FALLBACK_VIEWPORT_HEIGHT = 320
const OVERSCAN_ROWS = 6
const OVERSCAN_PX = SEGMENT_ROW_HEIGHT * 8

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max)
}

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

function buildSearchMeta(filtered: FilteredItem[], effectiveQuery: string, searchStartIndex: number): Map<number, SearchMeta> {
  const meta = new Map<number, SearchMeta>()
  let cursor = searchStartIndex
  for (const { seg, idx } of filtered) {
    const matchCount = countCaseInsensitiveMatches(seg.text, effectiveQuery)
    meta.set(idx, { start: cursor, matchCount })
    cursor += matchCount
  }
  return meta
}

function buildTranscriptRows(
  filtered: FilteredItem[],
  speakerIndexMap: Record<string, number>,
  speakerAliases: Record<string, string> | undefined,
): {
  rows: TranscriptRow[]
  segmentRowIndexByRawIdx: Map<number, number>
} {
  const rows: TranscriptRow[] = []
  const segmentRowIndexByRawIdx = new Map<number, number>()
  let currentSpeaker: string | null = null

  for (const { seg, idx } of filtered) {
    const speaker = seg.speaker != null && seg.speaker !== '' ? seg.speaker : null
    if (speaker !== currentSpeaker) {
      currentSpeaker = speaker
      if (speaker != null) {
        rows.push({
          kind: 'speaker',
          key: `speaker-${idx}-${speaker}`,
          speaker,
          displayName: speakerAliases?.[speaker] ?? speaker,
          accentIdx: speakerIndexMap[speaker] ?? 0,
          height: SPEAKER_ROW_HEIGHT,
        })
      }
    }

    segmentRowIndexByRawIdx.set(idx, rows.length)
    rows.push({
      kind: 'segment',
      key: `segment-${idx}`,
      idx,
      seg,
      speaker,
      height: SEGMENT_ROW_HEIGHT,
    })
  }

  return { rows, segmentRowIndexByRawIdx }
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

  const searchMeta = useMemo(
    () => buildSearchMeta(filtered, effectiveQuery, searchStartIndex),
    [effectiveQuery, filtered, searchStartIndex],
  )

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

  const { rows, segmentRowIndexByRawIdx } = useMemo(
    () => buildTranscriptRows(filtered, speakerIndexMap, speakerAliases),
    [filtered, speakerAliases, speakerIndexMap],
  )
  const virtualized = rows.length > VIRTUALIZE_AFTER_SEGMENTS
  const rowLayout = useMemo(() => {
    let top = 0
    const positions = rows.map((row) => {
      const item = { ...row, top }
      top += row.height
      return item
    })
    return { positions, totalHeight: top }
  }, [rows])

  const citedRef = useRef<HTMLLIElement | null>(null)
  const pendingScroll = useRef(false)
  const viewportRef = useRef<HTMLDivElement | null>(null)
  const [scrollTop, setScrollTop] = useState(0)
  const [viewportHeight, setViewportHeight] = useState(FALLBACK_VIEWPORT_HEIGHT)
  const hasHighlight =
    highlightIndex != null && highlightIndex >= 0 && highlightIndex < segments.length

  useEffect(() => {
    if (!virtualized) return
    const node = viewportRef.current
    if (!node) return

    const updateViewportHeight = () => {
      setViewportHeight(node.clientHeight || FALLBACK_VIEWPORT_HEIGHT)
    }

    updateViewportHeight()

    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(updateViewportHeight)
    observer.observe(node)
    return () => observer.disconnect()
  }, [virtualized])

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
    if (virtualized && highlightIndex != null) {
      const rowIndex = segmentRowIndexByRawIdx.get(highlightIndex)
      if (rowIndex != null) {
        const { positions, totalHeight } = rowLayout
        const row = positions[rowIndex]
        const maxScrollTop = Math.max(totalHeight - viewportHeight, 0)
        const nextScrollTop = clamp(
          row.top - Math.max((viewportHeight - row.height) / 2, 0),
          0,
          maxScrollTop,
        )
        viewportRef.current?.scrollTo?.({ top: nextScrollTop })
        if (viewportRef.current) viewportRef.current.scrollTop = nextScrollTop
        setScrollTop(nextScrollTop)
      }
    }
    const el = citedRef.current
    if (!el) return
    pendingScroll.current = false
    if (typeof el.scrollIntoView === 'function') {
      el.scrollIntoView({ block: 'center' })
    }
  }, [hasHighlight, highlightIndex, rowLayout, segmentRowIndexByRawIdx, virtualized, viewportHeight, scrollTop])

  const virtualWindow = useMemo(() => {
    if (!virtualized) return { start: 0, end: rows.length, topSpacer: 0, bottomSpacer: 0 }

    const totalHeight = rowLayout.totalHeight
    const startPx = Math.max(scrollTop - OVERSCAN_PX, 0)
    const endPx = scrollTop + viewportHeight + OVERSCAN_PX

    let start = 0
    while (start < rowLayout.positions.length && rowLayout.positions[start].top + rowLayout.positions[start].height < startPx) {
      start++
    }

    let end = start
    while (end < rowLayout.positions.length && rowLayout.positions[end].top < endPx) {
      end++
    }

    start = Math.max(0, start - OVERSCAN_ROWS)
    end = Math.min(rowLayout.positions.length, end + OVERSCAN_ROWS)

    return {
      start,
      end,
      topSpacer: rowLayout.positions[start]?.top ?? 0,
      bottomSpacer: Math.max(totalHeight - (rowLayout.positions[end - 1]?.top ?? 0) - (rowLayout.positions[end - 1]?.height ?? 0), 0),
    }
  }, [rowLayout, rows.length, scrollTop, virtualized, viewportHeight])

  const renderSegmentRow = (row: Extract<TranscriptRow, { kind: 'segment' }>) => {
    const search = searchMeta.get(row.idx) ?? { start: searchStartIndex, matchCount: 0 }
    const cited = hasHighlight && row.idx === highlightIndex
    const playing = playingIndex != null && row.idx === playingIndex
    const highlighted = highlightSearchText(row.seg.text, effectiveQuery, search.start, searchCurrentIndex ?? null)
    const interactiveClass = cn(
      'flex w-full gap-3 rounded-[var(--radius)] text-sm text-left',
      onSeek && 'cursor-pointer',
      cited && 'bg-primary/10 ring-2 ring-primary',
      playing && 'bg-emerald-500/10 ring-1 ring-emerald-500/40',
    )
    const content = (
      <>
        <span className="shrink-0 font-mono tabular-nums text-muted-foreground">{ts(row.seg.start_ms)}</span>
        <span>{highlighted.nodes}</span>
      </>
    )
    return (
      <li
        key={row.key}
        ref={cited ? citedRef : undefined}
        data-cited={cited ? 'true' : undefined}
        data-playing={playing ? 'true' : undefined}
      >
        {onSeek ? (
          <button
            type="button"
            onClick={() => onSeek(row.seg.start_ms)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                onSeek(row.seg.start_ms)
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
  }

  if (segments.length === 0) {
    return <p className="text-sm text-muted-foreground">No transcript yet.</p>
  }

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
      ) : virtualized ? (
        <div
          ref={viewportRef}
          onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)}
          className="max-h-[24rem] overflow-y-auto pr-1"
          data-transcript-viewport="true"
          data-testid="transcript-viewport"
        >
          <div style={{ height: rowLayout.totalHeight, position: 'relative' }}>
            <div style={{ height: virtualWindow.topSpacer }} />
            <div className="flex flex-col gap-2">
              {(() => {
                const slice = rowLayout.positions.slice(virtualWindow.start, virtualWindow.end)
                const first = slice[0]
                const needsSyntheticHeading =
                  first?.kind === 'segment' &&
                  (virtualWindow.start === 0 || rowLayout.positions[virtualWindow.start - 1]?.kind !== 'speaker')
                const nodes = []
                if (needsSyntheticHeading) {
                  const speaker = first.speaker
                  if (speaker != null) {
                    nodes.push(
                      <div
                        key={`synthetic-speaker-${first.idx}`}
                        className="mb-1 border-l-2 pl-2 text-xs font-semibold uppercase tracking-wide"
                        style={speakerStyle((speakerIndexMap[speaker] ?? 0))}
                        data-speaker={speaker}
                        data-speaker-accent={speakerIndexMap[speaker] ?? 0}
                      >
                        {speakerAliases?.[speaker] ?? speaker}
                      </div>,
                    )
                  }
                }

                for (const row of slice) {
                  if (row.kind === 'speaker') {
                    nodes.push(
                      <div
                        key={row.key}
                        className="mb-1 border-l-2 pl-2 text-xs font-semibold uppercase tracking-wide"
                        style={speakerStyle(row.accentIdx)}
                        data-speaker={row.speaker}
                        data-speaker-accent={row.accentIdx}
                      >
                        {row.displayName}
                      </div>,
                    )
                  } else {
                    nodes.push(
                      <ul key={row.key} className="flex flex-col gap-2">
                        {renderSegmentRow(row)}
                      </ul>,
                    )
                  }
                }

                return nodes
              })()}
            </div>
            <div style={{ height: virtualWindow.bottomSpacer }} />
          </div>
        </div>
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
                  {group.items.map(({ seg, idx }) => renderSegmentRow({
                    kind: 'segment',
                    key: `segment-${idx}`,
                    idx,
                    seg,
                    speaker: seg.speaker != null && seg.speaker !== '' ? seg.speaker : null,
                    height: SEGMENT_ROW_HEIGHT,
                  }))}
                </ul>
              </div>
            )
          })}
        </div>
      ) : (
        <ul className="flex flex-col gap-2">
          {filtered.map(({ seg, idx }) => renderSegmentRow({
            kind: 'segment',
            key: `segment-${idx}`,
            idx,
            seg,
            speaker: seg.speaker != null && seg.speaker !== '' ? seg.speaker : null,
            height: SEGMENT_ROW_HEIGHT,
          }))}
        </ul>
      )}
    </div>
  )
}
