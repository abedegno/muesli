import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
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
    }
  | {
      kind: 'segment'
      key: string
      idx: number
      seg: TranscriptSegment
      speaker: string | null
    }

type SearchMeta = { start: number; matchCount: number }

const VIRTUALIZE_AFTER_SEGMENTS = 200
// Initial estimates ONLY. A row's real height is whatever its text, its
// wrapping and its spacing produce, so every rendered row is measured after
// layout and the measurement replaces the estimate (see `measured` below).
// These decide where rows that have never been rendered are assumed to be, and
// nothing else. Do not derive layout arithmetic from them.
const SEGMENT_ROW_HEIGHT = 44
const SPEAKER_ROW_HEIGHT = 24
const FALLBACK_VIEWPORT_HEIGHT = 320
const OVERSCAN_ROWS = 6
// The repeated speaker heading rendered when the window opens mid-group is
// extra content with no row of its own, so it is measured under its own key.
const SYNTHETIC_HEADING_KEY = 'synthetic-speaker-heading'

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

  // Measured height of every row that has been rendered at least once, keyed by
  // row key. Rows keep their measured height while they are scrolled out of the
  // window, so the totals stay stable instead of snapping back to an estimate.
  const [measured, setMeasured] = useState<Record<string, number>>({})

  // Row keys are positional (`segment-<idx>`), so the same key means a
  // different line once the transcript changes -- and the note screen reuses
  // this component instance when you open another note: the route carries no
  // `key` and the loader does not clear the previous note first. Without this
  // reset, note B is laid out from note A's heights for every row it has not
  // yet rendered.
  //
  // The basis is deliberately not the segments array: polling a processing note
  // hands over a fresh array with identical content on every tick, and keying
  // on identity would throw away every measurement each time.
  const measurementBasis = `${segments.length}:${segments[0]?.text ?? ''}`
  const [measuredBasis, setMeasuredBasis] = useState(measurementBasis)
  if (measurementBasis !== measuredBasis) {
    // Adjusting state during render, rather than in an effect: an effect would
    // commit one frame laid out with the previous transcript's heights first.
    setMeasuredBasis(measurementBasis)
    setMeasured({})
  }
  const rowRefs = useRef(new Map<string, HTMLElement>())

  const observeRow = useCallback((key: string, node: HTMLElement | null) => {
    if (node) rowRefs.current.set(key, node)
    else rowRefs.current.delete(key)
  }, [])

  // Measure after layout and before paint. The measured element is the row's
  // whole slot -- its spacing is padding inside it, not a gap between siblings
  // -- so the heights here add up to exactly what the rows occupy on screen.
  // Environments without layout (jsdom) report 0 and are skipped, which leaves
  // the estimates standing rather than collapsing every row to nothing.
  //
  // Deliberately unconditional: any render can mount a row that has never been
  // measured, so there is no dependency list that means "after every render".
  // It converges rather than looping -- a re-measurement that agrees with the
  // stored height sets no state.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useLayoutEffect(() => {
    if (!virtualized) return
    const next: Record<string, number> = {}
    let changed = false
    for (const [key, node] of rowRefs.current) {
      const height = node.getBoundingClientRect().height
      if (height <= 0) continue
      next[key] = height
      if (Math.abs((measured[key] ?? 0) - height) > 0.5) changed = true
    }
    if (changed) setMeasured((prev) => ({ ...prev, ...next }))
  })

  // Rows that have never been rendered are estimated from the mean of the rows
  // that have, per kind -- a speaker heading and a segment are not the same
  // shape. The constants only seed that mean: once one row of a kind has been
  // measured they stop influencing the layout. A fixed constant is a poor
  // estimator, and the further it sits from the truth the more scrolling to the
  // end becomes a treadmill, since every window measured on the way there grows
  // the total and pushes the end further away.
  const rowLayout = useMemo(() => {
    let segmentTotal = 0
    let segmentCount = 0
    let speakerTotal = 0
    let speakerCount = 0
    for (const row of rows) {
      const height = measured[row.key]
      if (height == null) continue
      if (row.kind === 'segment') {
        segmentTotal += height
        segmentCount++
      } else {
        speakerTotal += height
        speakerCount++
      }
    }
    const segmentEstimate = segmentCount > 0 ? segmentTotal / segmentCount : SEGMENT_ROW_HEIGHT
    const speakerEstimate = speakerCount > 0 ? speakerTotal / speakerCount : SPEAKER_ROW_HEIGHT

    let top = 0
    const positions = rows.map((row) => {
      const height =
        measured[row.key] ?? (row.kind === 'segment' ? segmentEstimate : speakerEstimate)
      const item = { ...row, height, top }
      top += height
      return item
    })
    return { positions, totalHeight: top }
  }, [measured, rows])

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
    if (!virtualized) {
      return { start: 0, end: rows.length, topSpacer: 0, bottomSpacer: 0, syntheticHeading: false }
    }

    const totalHeight = rowLayout.totalHeight
    // A scroll buffer, not a row measurement: how far outside the visible band
    // rows stay mounted so a fast scroll does not reveal blank space. It is
    // sized from the viewport — the thing actually being scrolled — rather than
    // from any assumption about how tall a row is.
    const overscanPx = viewportHeight
    const startPx = Math.max(scrollTop - overscanPx, 0)
    const endPx = scrollTop + viewportHeight + overscanPx

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

    const first = rowLayout.positions[start]
    const syntheticHeading =
      first?.kind === 'segment' &&
      first.speaker != null &&
      (start === 0 || rowLayout.positions[start - 1]?.kind !== 'speaker')

    // The repeated heading is content the row layout knows nothing about, so
    // take its height out of the spacer above it. Without that the rendered
    // block is taller than totalHeight claims and every offset below it is off
    // by one heading.
    const headingHeight = syntheticHeading ? (measured[SYNTHETIC_HEADING_KEY] ?? 0) : 0

    return {
      start,
      end,
      syntheticHeading,
      topSpacer: Math.max((rowLayout.positions[start]?.top ?? 0) - headingHeight, 0),
      bottomSpacer: Math.max(totalHeight - (rowLayout.positions[end - 1]?.top ?? 0) - (rowLayout.positions[end - 1]?.height ?? 0), 0),
    }
  }, [measured, rowLayout, rows.length, scrollTop, virtualized, viewportHeight])

  const renderSegmentRow = (row: Extract<TranscriptRow, { kind: 'segment' }>) => {
    const search = searchMeta.get(row.idx) ?? { start: searchStartIndex, matchCount: 0 }
    const cited = hasHighlight && row.idx === highlightIndex
    const playing = playingIndex != null && row.idx === playingIndex
    const highlighted = highlightSearchText(row.seg.text, effectiveQuery, search.start, searchCurrentIndex ?? null)
    const interactiveClass = cn(
      'flex w-full gap-3 rounded-[var(--radius)] text-sm text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
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
            {/*
              No `gap` here: a gap is space between siblings that belongs to no
              element, so nothing can measure it. Each row carries its own
              trailing space as padding instead, which keeps the measured height
              equal to the height the row occupies.
            */}
            <div className="flex flex-col">
              {(() => {
                const slice = rowLayout.positions.slice(virtualWindow.start, virtualWindow.end)
                const first = slice[0]
                const nodes = []
                if (virtualWindow.syntheticHeading && first?.kind === 'segment') {
                  const speaker = first.speaker
                  if (speaker != null) {
                    nodes.push(
                      <div
                        key={`synthetic-speaker-${first.idx}`}
                        ref={(node) => observeRow(SYNTHETIC_HEADING_KEY, node)}
                        className="pb-3"
                      >
                        <div
                          className="border-l-2 pl-2 text-xs font-semibold uppercase tracking-wide"
                          style={speakerStyle((speakerIndexMap[speaker] ?? 0))}
                          data-speaker={speaker}
                          data-speaker-accent={speakerIndexMap[speaker] ?? 0}
                        >
                          {speakerAliases?.[speaker] ?? speaker}
                        </div>
                      </div>,
                    )
                  }
                }

                for (const row of slice) {
                  if (row.kind === 'speaker') {
                    nodes.push(
                      <div
                        key={row.key}
                        ref={(node) => observeRow(row.key, node)}
                        className="pb-3"
                      >
                        <div
                          className="border-l-2 pl-2 text-xs font-semibold uppercase tracking-wide"
                          style={speakerStyle(row.accentIdx)}
                          data-speaker={row.speaker}
                          data-speaker-accent={row.accentIdx}
                        >
                          {row.displayName}
                        </div>
                      </div>,
                    )
                  } else {
                    nodes.push(
                      <ul
                        key={row.key}
                        ref={(node) => observeRow(row.key, node)}
                        className="flex flex-col pb-2"
                      >
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
          }))}
        </ul>
      )}
    </div>
  )
}
