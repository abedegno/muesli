// @vitest-environment jsdom
import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest'
import { render, screen, cleanup, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TranscriptView } from './TranscriptView'
import type { TranscriptSegment } from '../../shared/types'

// jsdom has no scrollIntoView; install a mock so the scroll path is observable and never throws.
beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn()
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  // restoreAllMocks doesn't remove a directly-assigned prototype property; delete it so other
  // tests run against pristine jsdom.
  delete (Element.prototype as unknown as { scrollIntoView?: unknown }).scrollIntoView
})

const segs: TranscriptSegment[] = [
  { start_ms: 0,     end_ms: 1000,  text: 'hello world', source: 'mixed' },
  { start_ms: 5000,  end_ms: 6000,  text: 'foo bar baz',  source: 'mixed' },
  { start_ms: 10000, end_ms: 11000, text: 'another line', source: 'mixed' },
]

// Fixture for diarization tests: 4 segments, 2 distinct speakers, 2 consecutive same-speaker
// segments (Speaker 1 at index 0+1), and at least one speaker change (index 1→2 and 2→3).
const speakerSegs: TranscriptSegment[] = [
  { start_ms: 0,    end_ms: 1000, text: 'hello from speaker one', source: 'mixed', speaker: 'Speaker 1' },
  { start_ms: 1000, end_ms: 2000, text: 'still speaker one here', source: 'mixed', speaker: 'Speaker 1' },
  { start_ms: 2000, end_ms: 3000, text: 'now speaker two speaks', source: 'mixed', speaker: 'Speaker 2' },
  { start_ms: 3000, end_ms: 4000, text: 'speaker one returns',    source: 'mixed', speaker: 'Speaker 1' },
]

const largeSegs: TranscriptSegment[] = Array.from({ length: 1000 }, (_, i) => ({
  start_ms: i * 1000,
  end_ms: (i + 1) * 1000,
  text: i === 837 ? `segment ${i} budget focus` : `segment ${i}`,
  source: 'mixed',
}))

describe('TranscriptView', () => {
  it('shows empty state when no segments', () => {
    render(<TranscriptView segments={[]} />)
    expect(screen.getByText('No transcript yet.')).toBeInTheDocument()
    expect(screen.queryByLabelText('Search transcript')).not.toBeInTheDocument()
  })

  it('renders all segments and a search input when segments are present', () => {
    render(<TranscriptView segments={segs} />)
    expect(screen.getByLabelText('Search transcript')).toBeInTheDocument()
    expect(screen.getByText('hello world')).toBeInTheDocument()
    expect(screen.getByText('foo bar baz')).toBeInTheDocument()
    expect(screen.getByText('another line')).toBeInTheDocument()
  })

  it('filters to matching segments only', async () => {
    render(<TranscriptView segments={segs} />)
    await userEvent.type(screen.getByLabelText('Search transcript'), 'foo')
    expect(screen.getByText(/foo/)).toBeInTheDocument()
    expect(screen.queryByText('hello world')).not.toBeInTheDocument()
    expect(screen.queryByText('another line')).not.toBeInTheDocument()
  })

  it('wraps matched text in a <mark> element', async () => {
    render(<TranscriptView segments={segs} />)
    await userEvent.type(screen.getByLabelText('Search transcript'), 'foo')
    const mark = document.querySelector('mark')
    expect(mark).not.toBeNull()
    expect(mark!.textContent).toBe('foo')
  })

  it('is case-insensitive (uppercase query matches lowercase text)', async () => {
    render(<TranscriptView segments={segs} />)
    await userEvent.type(screen.getByLabelText('Search transcript'), 'WORLD')
    expect(screen.getByText(/world/i)).toBeInTheDocument()
    expect(screen.queryByText('foo bar baz')).not.toBeInTheDocument()
    const mark = document.querySelector('mark')
    expect(mark).not.toBeNull()
    expect(mark!.textContent).toBe('world')
  })

  it('shows "No matching lines." when query has no matches', async () => {
    render(<TranscriptView segments={segs} />)
    await userEvent.type(screen.getByLabelText('Search transcript'), 'xyzzy')
    expect(screen.getByText('No matching lines.')).toBeInTheDocument()
    expect(screen.queryByText('hello world')).not.toBeInTheDocument()
  })

  it('shows all segments again after clearing the query', async () => {
    render(<TranscriptView segments={segs} />)
    const input = screen.getByLabelText('Search transcript')
    await userEvent.type(input, 'foo')
    expect(screen.queryByText('hello world')).not.toBeInTheDocument()
    await userEvent.clear(input)
    expect(screen.getByText('hello world')).toBeInTheDocument()
    expect(screen.getByText('foo bar baz')).toBeInTheDocument()
    expect(screen.getByText('another line')).toBeInTheDocument()
  })

  it('applies a highlight class to the segment at highlightIndex', () => {
    render(<TranscriptView segments={segs} highlightIndex={1} />)
    const highlighted = document.querySelector('[data-cited="true"]')
    expect(highlighted).not.toBeNull()
    expect(highlighted!.textContent).toContain('foo bar baz')
    // Other segments are not marked cited
    expect(document.querySelectorAll('[data-cited="true"]')).toHaveLength(1)
  })

  it('invokes onSeek with the clicked segment start time', async () => {
    const onSeek = vi.fn()
    render(<TranscriptView segments={segs} onSeek={onSeek} />)
    expect(screen.getByRole('button', { name: /foo bar baz/i })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /foo bar baz/i }))
    expect(onSeek).toHaveBeenCalledWith(5000)
  })

  it('marks the playing segment with a distinct playing marker', () => {
    render(<TranscriptView segments={segs} playingIndex={2} />)
    const playing = document.querySelector('[data-playing="true"]')
    expect(playing).not.toBeNull()
    expect(playing!.textContent).toContain('another line')
  })

  it('marks no segment cited when highlightIndex is null', () => {
    render(<TranscriptView segments={segs} highlightIndex={null} />)
    expect(document.querySelectorAll('[data-cited="true"]')).toHaveLength(0)
  })

  it('does not crash on an out-of-range highlightIndex', () => {
    expect(() => render(<TranscriptView segments={segs} highlightIndex={99} />)).not.toThrow()
    expect(document.querySelectorAll('[data-cited="true"]')).toHaveLength(0)
    expect(screen.getByText('hello world')).toBeInTheDocument()
  })

  it('scrolls to the cited segment when there is no active filter (no-filter path unchanged)', async () => {
    const scroll = Element.prototype.scrollIntoView as ReturnType<typeof vi.fn>
    render(<TranscriptView segments={segs} highlightIndex={1} />)
    const cited = document.querySelector('[data-cited="true"]')
    expect(cited).not.toBeNull()
    expect(cited!.textContent).toContain('foo bar baz')
    await waitFor(() => expect(scroll).toHaveBeenCalled())
  })

  it('citation click with an active filter clears the filter and scrolls to the cited segment', async () => {
    const scroll = Element.prototype.scrollIntoView as ReturnType<typeof vi.fn>
    const { rerender } = render(<TranscriptView segments={segs} highlightIndex={null} />)

    // Type a needle matching only 'hello world' (index 0), excluding the segment we will cite.
    const input = screen.getByLabelText('Search transcript') as HTMLInputElement
    await userEvent.type(input, 'hello')
    expect(input.value).toBe('hello')
    // The segment we will cite (index 1, 'foo bar baz') is filtered out of the DOM.
    expect(screen.queryByText('foo bar baz')).not.toBeInTheDocument()

    // Citation arrives for the excluded segment (full-array index 1).
    rerender(<TranscriptView segments={segs} highlightIndex={1} />)

    // Filter is cleared, cited segment re-enters the DOM and is scrolled into view.
    await waitFor(() => expect(input.value).toBe(''))
    expect(input).toHaveValue('')
    const cited = await screen.findByText('foo bar baz')
    expect(cited.closest('[data-cited="true"]')).not.toBeNull()
    await waitFor(() => expect(scroll).toHaveBeenCalled())
  })

  it('does not scroll on plain filter typing when no new citation was clicked', async () => {
    const scroll = Element.prototype.scrollIntoView as ReturnType<typeof vi.fn>
    render(<TranscriptView segments={segs} highlightIndex={null} />)
    await userEvent.type(screen.getByLabelText('Search transcript'), 'foo')
    expect(scroll).not.toHaveBeenCalled()
  })

  it('virtualizes a large transcript and keeps the rendered segment DOM bounded', () => {
    render(<TranscriptView segments={largeSegs} />)
    const renderedSegments = document.querySelectorAll('li[data-cited], li[data-playing], li').length
    expect(renderedSegments).toBeLessThan(1000)
    expect(renderedSegments).toBeLessThan(100)
  })

  it('scrolls an off-window citation into view and highlights it', async () => {
    const { rerender } = render(<TranscriptView segments={largeSegs} highlightIndex={null} />)
    const viewport = screen.getByTestId('transcript-viewport')
    viewport.scrollTop = 12000
    fireEvent.scroll(viewport)

    rerender(<TranscriptView segments={largeSegs} highlightIndex={837} />)

    const cited = await screen.findByText(/segment 837 budget focus/)
    await waitFor(() => expect(cited.closest('[data-cited="true"]')).not.toBeNull())
    expect(viewport.scrollTop).toBeGreaterThan(12000)
  })

  it('filters the rendered set and highlights search matches in the filtered list', async () => {
    render(<TranscriptView segments={largeSegs} />)
    await userEvent.type(screen.getByLabelText('Search transcript'), 'budget')

    expect(document.querySelectorAll('li')).toHaveLength(1)
    expect(screen.queryByText('segment 0')).not.toBeInTheDocument()
    const marks = document.querySelectorAll('mark')
    expect(marks).toHaveLength(1)
    expect(marks[0].textContent).toBe('budget')
  })

  it('regresses if citation scrolling relies on the target already being rendered', async () => {
    const { rerender } = render(<TranscriptView segments={largeSegs} highlightIndex={900} />)
    const viewport = screen.getByTestId('transcript-viewport')
    expect(viewport.scrollTop).toBeGreaterThan(0)

    viewport.scrollTop = 0
    fireEvent.scroll(viewport)
    expect(screen.queryByText('segment 900')).not.toBeInTheDocument()

    rerender(<TranscriptView segments={largeSegs} highlightIndex={12} />)

    const cited = await screen.findByText('segment 12')
    await waitFor(() => expect(cited.closest('[data-cited="true"]')).not.toBeNull())
    expect(viewport.scrollTop).toBeLessThan(2000)
  })
})

describe('TranscriptView — speaker attribution', () => {
  it('renders speaker headings when segments have speaker labels', () => {
    render(<TranscriptView segments={speakerSegs} />)
    // Both distinct speakers appear as headings in the DOM
    expect(screen.getAllByText('Speaker 1').length).toBeGreaterThan(0)
    expect(screen.getByText('Speaker 2')).toBeInTheDocument()
  })

  it('consecutive same-speaker segments share one heading per run, not one per segment', () => {
    render(<TranscriptView segments={speakerSegs} />)
    // 3 runs total: [Speaker 1 × 2], [Speaker 2 × 1], [Speaker 1 × 1]
    // → 2 Speaker 1 headings (not 3 if each segment got its own)
    const s1headings = document.querySelectorAll('[data-speaker="Speaker 1"]')
    expect(s1headings).toHaveLength(2)
    // Speaker 2 appears once
    const s2headings = document.querySelectorAll('[data-speaker="Speaker 2"]')
    expect(s2headings).toHaveLength(1)
    // All segment texts are still rendered
    expect(screen.getByText('hello from speaker one')).toBeInTheDocument()
    expect(screen.getByText('still speaker one here')).toBeInTheDocument()
    expect(screen.getByText('now speaker two speaks')).toBeInTheDocument()
    expect(screen.getByText('speaker one returns')).toBeInTheDocument()
  })

  it('different speakers get different accent indices (stable colour)', () => {
    render(<TranscriptView segments={speakerSegs} />)
    const s1heading = document.querySelector('[data-speaker="Speaker 1"]')
    const s2heading = document.querySelector('[data-speaker="Speaker 2"]')
    expect(s1heading).not.toBeNull()
    expect(s2heading).not.toBeNull()
    // Speaker 1 and Speaker 2 must get different palette indices
    expect(s1heading!.getAttribute('data-speaker-accent')).not.toBe(
      s2heading!.getAttribute('data-speaker-accent'),
    )
    // The same speaker always gets the same accent index (stable across runs)
    const allS1 = document.querySelectorAll('[data-speaker="Speaker 1"]')
    const accents = Array.from(allS1).map(el => el.getAttribute('data-speaker-accent'))
    expect(new Set(accents).size).toBe(1)
  })

  it('renders no speaker headings when no segment has a speaker (regression guard)', () => {
    render(<TranscriptView segments={segs} />)
    expect(document.querySelector('[data-speaker]')).toBeNull()
    // All segment texts still present
    expect(screen.getByText('hello world')).toBeInTheDocument()
    expect(screen.getByText('foo bar baz')).toBeInTheDocument()
    expect(screen.getByText('another line')).toBeInTheDocument()
  })

  it('speakerAliases prop: shows alias instead of raw speaker label', () => {
    render(<TranscriptView segments={speakerSegs} speakerAliases={{ 'Speaker 1': 'Alice' }} />)
    // 'Alice' should appear in place of 'Speaker 1'
    expect(screen.getAllByText('Alice').length).toBeGreaterThan(0)
    // Raw label 'Speaker 1' must NOT appear as visible text
    expect(screen.queryByText('Speaker 1')).not.toBeInTheDocument()
    // Speaker 2 (no alias) still shows raw label
    expect(screen.getByText('Speaker 2')).toBeInTheDocument()
  })

  it('golden-angle: 7 speakers all get distinct accent indices', () => {
    const sevenSpeakers: TranscriptSegment[] = Array.from({ length: 7 }, (_, i) => ({
      start_ms: i * 1000, end_ms: (i + 1) * 1000,
      text: `line ${i}`, source: 'mixed', speaker: `Speaker ${i + 1}`,
    }))
    render(<TranscriptView segments={sevenSpeakers} />)
    const accents = Array.from(document.querySelectorAll('[data-speaker-accent]'))
      .map(el => el.getAttribute('data-speaker-accent'))
    // All 7 accents must be unique (indices 0-6)
    expect(new Set(accents).size).toBe(7)
  })
})
