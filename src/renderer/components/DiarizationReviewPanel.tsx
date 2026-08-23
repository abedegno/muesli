import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent } from 'react'
import { Users, X } from 'lucide-react'
import { muesli } from '@/api'
import { useAnnouncer } from '@/hooks/useAnnouncer'
import { cn } from '@/lib/cn'
import { isConflictError } from '@/lib/apiError'
import type { DiarizationReview, TranscriptSegment } from '../../shared/types'

// A review turn requires an `id` (the segment_id the POST endpoint keys off).
// Turns without one (shouldn't happen from a well-formed server, but be
// defensive) are dropped rather than rendered as unconfirmable dead rows.
interface Turn {
  seg: TranscriptSegment
  id: string
  // The speaker currently selected for this turn — starts as the server's
  // reported speaker, and can be cycled through `alternatives` locally before
  // being confirmed (POSTed) as the turn's actual speaker.
  selectedSpeaker: string
  confirmed: boolean
}

// Shown, announced and returned from every path that hits a 409. The wording
// has to say that the edit was NOT saved: a refetch that silently re-rendered
// would look like success.
const CONFLICT_MESSAGE =
  'This transcript was replaced while you were reviewing it. Your change was not saved, and the turns below have been reloaded from the new transcript.'

function toTurns(review: DiarizationReview): Turn[] {
  return review.turns
    .filter((seg): seg is TranscriptSegment & { id: string } => !!seg.id)
    .map((seg) => ({ seg, id: seg.id, selectedSpeaker: seg.speaker ?? '', confirmed: false }))
}

// Distinct speaker labels seen across the review's turns, in first-appearance
// order — there is no separate server-side candidate list (DZ04d scope note).
function distinctSpeakers(review: DiarizationReview): string[] {
  const out: string[] = []
  for (const seg of review.turns) {
    if (seg.speaker && !out.includes(seg.speaker)) out.push(seg.speaker)
  }
  return out
}

/**
 * Entry point + modal panel for reviewing a note's low-confidence diarization
 * turns (DZ04d), backed by the DZ04b GET/POST /transcript/review endpoints.
 *
 * Renders nothing (no button) when there's no transcript, the review fetch
 * fails, or the review is already `completed`.
 */
export function DiarizationReviewPanel({
  noteId,
  hasTranscript,
}: {
  noteId: string
  hasTranscript: boolean
}) {
  const [review, setReview] = useState<DiarizationReview | null>(null)
  const [open, setOpen] = useState(false)
  const [turns, setTurns] = useState<Turn[]>([])
  const [activeIndex, setActiveIndex] = useState(0)
  const [busy, setBusy] = useState(false)
  const [conflict, setConflict] = useState(false)
  const itemRefs = useRef<Array<HTMLLIElement | null>>([])
  const { announce } = useAnnouncer()

  // (Re)fetch the review whenever the note changes. Guarded against a bridge
  // that doesn't expose the method (e.g. component tests that mock only part
  // of `@/api`) — treated the same as a failed fetch: hide the entry point.
  useEffect(() => {
    setReview(null)
    setOpen(false)
    setConflict(false)
    if (!hasTranscript) return
    const get = muesli?.getDiarizationReview
    if (typeof get !== 'function') return
    let cancelled = false
    get(noteId)
      .then((r) => { if (!cancelled) setReview(r) })
      .catch(() => { if (!cancelled) setReview(null) })
    return () => { cancelled = true }
  }, [noteId, hasTranscript])

  const speakers = useMemo(() => (review ? distinctSpeakers(review) : []), [review])

  // Refetch after a 409 and surface it. The refetched review replaces the
  // panel's state so later actions carry a generation that exists — but the
  // rejected edit is deliberately NOT resubmitted. Retrying it silently would
  // apply the user's change to a transcript they never saw, which is the
  // mutation the generation guard exists to prevent.
  const handleConflict = useCallback(async () => {
    setConflict(true)
    announce(CONFLICT_MESSAGE)
    const get = muesli?.getDiarizationReview
    if (typeof get !== 'function') return
    try {
      const fresh = await get(noteId)
      setReview(fresh)
      setTurns(toTurns(fresh))
      setActiveIndex(0)
    } catch {
      // The replacement is not reviewable (no transcript, or it needs no
      // review). Nothing is left to show, so close rather than leave a panel
      // bound to a transcript that no longer exists.
      setReview(null)
      setOpen(false)
    }
  }, [noteId, announce])

  const openPanel = useCallback(() => {
    if (!review) return
    setTurns(toTurns(review))
    setActiveIndex(0)
    setConflict(false)
    setOpen(true)
    if (review.review_state === 'pending') {
      // Best-effort lifecycle transition — a 422 (e.g. a race with another
      // client) just leaves review_state as-is. A 409 is NOT best-effort: it
      // means the transcript this panel was rendered from is gone, so every
      // later action would 409 too unless the review is refetched here.
      // generation is the one this panel was just rendered from (review_state
      // transitions never change it), not a refetched value.
      muesli.postDiarizationReview(noteId, { reviewState: 'in_review', generation: review.generation })
        .then((r) => setReview(r))
        .catch((err: unknown) => {
          if (isConflictError(err)) void handleConflict()
        })
    }
  }, [review, noteId, handleConflict])

  // Move DOM focus to the active item whenever the panel opens.
  useEffect(() => {
    if (open) itemRefs.current[activeIndex]?.focus()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  // Not memoized: it closes over the current `turns` length, which changes
  // whenever a turn is confirmed/reassigned. Fresh on every render is correct
  // and cheap — it's only invoked from this render's own JSX event handlers.
  const focusAt = (i: number) => {
    if (turns.length === 0) return
    const n = turns.length
    const next = ((i % n) + n) % n
    setActiveIndex(next)
    itemRefs.current[next]?.focus()
  }

  const cycleSpeaker = useCallback((idx: number, dir: 1 | -1) => {
    if (speakers.length === 0) return
    setTurns((ts) => ts.map((t, i) => {
      if (i !== idx) return t
      const cur = speakers.indexOf(t.selectedSpeaker)
      const nextIdx = ((cur === -1 ? 0 : cur) + dir + speakers.length) % speakers.length
      const nextSpeaker = speakers[nextIdx]
      announce(`Turn ${idx + 1} reassigned to ${nextSpeaker}`)
      return { ...t, selectedSpeaker: nextSpeaker }
    }))
  }, [speakers, announce])

  const confirmTurn = useCallback(async (idx: number) => {
    const t = turns[idx]
    if (!t || !review) return
    setBusy(true)
    try {
      // generation is the one this panel was rendered from, not refetched —
      // a refetch here would defeat the point of the check, which is to
      // detect that the transcript moved under the user mid-review.
      await muesli.postDiarizationReview(noteId, { segmentId: t.id, speaker: t.selectedSpeaker, generation: review.generation })
      setTurns((ts) => ts.map((tt, i) => (i === idx ? { ...tt, confirmed: true } : tt)))
      announce(`Confirmed speaker ${t.selectedSpeaker || 'unknown'} for turn ${idx + 1}`)
    } catch (err) {
      // A 409 is not a failed request to retry — the transcript changed.
      if (isConflictError(err)) await handleConflict()
      else announce(`Could not confirm turn ${idx + 1}`)
    } finally {
      setBusy(false)
    }
  }, [turns, review, noteId, announce, handleConflict])

  const acceptAllRemaining = useCallback(async () => {
    if (!review) return
    const remaining = turns.filter((t) => !t.confirmed)
    if (remaining.length === 0) return
    setBusy(true)
    let count = 0
    let conflicted = false
    for (const t of remaining) {
      try {
        // Confirmations are sent sequentially (not Promise.all) so a consistent
        // server-side state is reached even if one request in the batch fails.
        // generation is the panel's rendered-from value, held constant across
        // the whole batch — not refetched between confirmations.
        await muesli.postDiarizationReview(noteId, { segmentId: t.id, speaker: t.selectedSpeaker, generation: review.generation })
        count++
        setTurns((ts) => ts.map((tt) => (tt.id === t.id ? { ...tt, confirmed: true } : tt)))
      } catch (err) {
        // A 409 applies to the whole batch: every remaining confirmation
        // carries the same now-dead generation, so continuing would send
        // requests that cannot succeed and report "Confirmed 0 of N" as though
        // the server had merely been flaky. Stop and refetch instead.
        if (isConflictError(err)) {
          conflicted = true
          break
        }
        // Any other failure is per-request — keep going and report in aggregate.
      }
    }
    setBusy(false)
    if (conflicted) {
      await handleConflict()
      return
    }
    announce(`Confirmed ${count} of ${remaining.length} remaining turn${remaining.length === 1 ? '' : 's'}`)
  }, [turns, review, noteId, announce, handleConflict])

  const completeReview = useCallback(async () => {
    if (!review) return
    setBusy(true)
    try {
      await muesli.postDiarizationReview(noteId, { reviewState: 'completed', generation: review.generation })
      announce('Diarization review completed')
      setReview(null) // hides the entry point
      setOpen(false)
    } catch (err) {
      if (isConflictError(err)) await handleConflict()
      else announce('Could not complete the review')
    } finally {
      setBusy(false)
    }
  }, [review, noteId, announce, handleConflict])

  const onItemKeyDown = (e: ReactKeyboardEvent<HTMLLIElement>, idx: number) => {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        focusAt(idx + 1)
        break
      case 'ArrowUp':
        e.preventDefault()
        focusAt(idx - 1)
        break
      case 'ArrowRight':
        e.preventDefault()
        cycleSpeaker(idx, 1)
        break
      case 'ArrowLeft':
        e.preventDefault()
        cycleSpeaker(idx, -1)
        break
      case 'Enter':
        e.preventDefault()
        void confirmTurn(idx)
        break
      default:
        break
    }
  }

  if (!hasTranscript || !review || review.review_state === 'completed') return null

  return (
    <>
      <button
        type="button"
        aria-label="Review speakers"
        onClick={openPanel}
        className="inline-flex items-center gap-1 rounded-[var(--radius)] px-3 py-1 text-sm font-medium text-muted-foreground hover:bg-muted"
      >
        <Users size={16} /> Review speakers
      </button>
      {open && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div
            role="dialog"
            aria-modal="true"
            aria-label="Diarization review"
            className="flex max-h-[80vh] w-[36rem] max-w-[92vw] flex-col overflow-hidden rounded-[var(--radius)] border border-border bg-popover shadow-md"
          >
            <div className="flex items-center justify-between border-b border-border px-4 py-3">
              <h2 className="text-sm font-semibold">Review speaker turns</h2>
              <button
                type="button"
                aria-label="Close review panel"
                onClick={() => setOpen(false)}
                className="text-muted-foreground hover:text-foreground"
              >
                <X size={16} />
              </button>
            </div>

            {conflict && (
              <p
                role="alert"
                data-testid="review-conflict"
                className="border-b border-border bg-muted px-4 py-3 text-sm text-foreground"
              >
                {CONFLICT_MESSAGE}
              </p>
            )}

            {turns.length === 0 ? (
              <p className="px-4 py-6 text-sm text-muted-foreground">No low-confidence turns to review.</p>
            ) : (
              <ul
                role="listbox"
                aria-label="Low-confidence speaker turns"
                className="flex-1 overflow-y-auto p-2"
              >
                {turns.map((t, idx) => (
                  <li
                    key={t.id}
                    ref={(el) => { itemRefs.current[idx] = el }}
                    role="option"
                    aria-selected={idx === activeIndex}
                    tabIndex={idx === activeIndex ? 0 : -1}
                    onFocus={() => setActiveIndex(idx)}
                    onKeyDown={(e) => onItemKeyDown(e, idx)}
                    className={cn(
                      'mb-2 flex flex-col gap-1 rounded-[var(--radius)] border border-border p-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                      t.confirmed && 'opacity-60',
                    )}
                  >
                    <p>{t.seg.text}</p>
                    <div className="flex flex-wrap items-center gap-1.5 text-xs">
                      <span className="text-muted-foreground">Speaker:</span>
                      <span className="font-medium" data-testid="current-speaker">{t.selectedSpeaker || '(unknown)'}</span>
                      {t.confirmed && <span className="text-primary">Confirmed</span>}
                    </div>
                    {speakers.length > 1 && (
                      <div className="flex flex-wrap gap-1">
                        {speakers.map((s) => (
                          <button
                            key={s}
                            type="button"
                            tabIndex={-1}
                            aria-label={`Reassign turn ${idx + 1} to ${s}`}
                            onClick={() => setTurns((ts) => ts.map((tt, i) => (i === idx ? { ...tt, selectedSpeaker: s } : tt)))}
                            className={cn(
                              'rounded border px-1.5 py-0.5',
                              s === t.selectedSpeaker ? 'border-primary text-primary' : 'border-border text-muted-foreground',
                            )}
                          >
                            {s}
                          </button>
                        ))}
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            )}

            <div className="flex items-center justify-between border-t border-border px-4 py-3">
              <button
                type="button"
                disabled={busy || turns.every((t) => t.confirmed)}
                onClick={() => void acceptAllRemaining()}
                className="rounded-[var(--radius)] px-3 py-1.5 text-sm font-medium text-muted-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
              >
                Accept all remaining
              </button>
              <button
                type="button"
                disabled={busy}
                onClick={() => void completeReview()}
                className="rounded-[var(--radius)] bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:cursor-not-allowed disabled:opacity-50"
              >
                Mark review complete
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
