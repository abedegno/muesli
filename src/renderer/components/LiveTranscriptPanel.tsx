import { useEffect, useRef, useState } from 'react'
import { muesli } from '@/api'
import type { NoteStreamEvent } from '../../shared/ipc'

type LiveState = 'idle' | 'connecting' | 'loading' | 'live' | 'unavailable' | 'dropped'

interface LiveTranscriptSource {
  onNoteStreamEvent?: (cb: (event: NoteStreamEvent) => void) => () => void
}

export function LiveTranscriptPanel({
  noteId,
  isRecording,
  source = muesli,
}: {
  noteId: string
  isRecording: boolean
  source?: LiveTranscriptSource
}) {
  const [status, setStatus] = useState<LiveState>('idle')
  const [segments, setSegments] = useState<Extract<NoteStreamEvent, { type: 'segment' }>[]>([])
  const [interimSegment, setInterimSegment] = useState<{
    text: string
    start_ms: number
    end_ms: number
    speaker: string | null
  } | null>(null)
  const bottomRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!isRecording) {
      setStatus('idle')
      setSegments([])
      setInterimSegment(null)
    } else {
      setStatus('connecting')
      setSegments([])
      setInterimSegment(null)
    }
  }, [isRecording])

  useEffect(() => {
    if (typeof source.onNoteStreamEvent !== 'function') return
    const unsubscribe = source.onNoteStreamEvent((event) => {
      if (event.noteId !== noteId) return
      if (event.type === 'segment') {
        setStatus('live')
        if (event.final === false) {
          setInterimSegment({
            text: event.text,
            start_ms: event.start_ms,
            end_ms: event.end_ms,
            speaker: event.speaker,
          })
          return
        }
        setSegments((current) => [...current, event])
        setInterimSegment(null)
        return
      }
      if (event.type === 'connecting') {
        setStatus('connecting')
        setSegments([])
        setInterimSegment(null)
        return
      }
      // Gap events are exposed to renderer consumers, but visual treatment is
      // intentionally deferred. Do not reinterpret them as connection state.
      if (event.type === 'gap') return
      setStatus(event.type)
      setSegments([])
      setInterimSegment(null)
    })
    return unsubscribe
  }, [noteId, source])

  useEffect(() => {
    if (status !== 'live') return
    bottomRef.current?.scrollIntoView({ block: 'end', behavior: 'smooth' })
  }, [interimSegment, segments, status])

  if (!isRecording || status === 'idle') return null

  if (status === 'unavailable' || status === 'dropped') {
    return (
      <div
        data-testid="live-transcript-unavailable"
        className="mx-6 rounded-[var(--radius)] border border-border/60 bg-muted/30 px-4 py-3 text-sm text-muted-foreground"
        role="status"
        aria-live="polite"
      >
        Live transcript unavailable — the full transcript will be ready after recording.
      </div>
    )
  }

  if (status === 'loading') {
    return (
      <section
        data-testid="live-transcript-panel"
        className="mx-6 rounded-[var(--radius)] border border-amber-500/40 bg-amber-500/5 px-4 py-3 shadow-sm"
        role="status"
        aria-live="polite"
      >
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <span className="inline-flex h-2 w-2 animate-pulse rounded-full bg-amber-500" aria-hidden />
          <span className="font-medium text-foreground">Live transcript</span>
          <span aria-hidden>·</span>
          <span>Starting</span>
        </div>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">
          Starting the transcription engine — this takes a few seconds the first time.
        </p>
      </section>
    )
  }

  return (
    <section data-testid="live-transcript-panel" className="mx-6 rounded-[var(--radius)] border border-border/70 bg-background/80 px-4 py-3 shadow-sm">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <span className="inline-flex h-2 w-2 rounded-full bg-emerald-500" aria-hidden />
        <span className="font-medium text-foreground">Live transcript</span>
        <span aria-hidden>·</span>
        <span>Live</span>
      </div>
      <div className="mt-3 space-y-3 text-sm leading-6 text-foreground">
        {segments.length === 0 && interimSegment === null ? (
          <p className="text-muted-foreground">Listening…</p>
        ) : (
          <>
            {segments.map((segment) => (
              <p key={`${segment.start_ms}-${segment.end_ms}-${segment.text}`} className="whitespace-pre-wrap">
                {segment.text}
              </p>
            ))}
            {interimSegment !== null ? (
              <p
                data-testid="live-transcript-interim"
                className="whitespace-pre-wrap italic text-muted-foreground/80"
              >
                {interimSegment.text}
              </p>
            ) : null}
          </>
        )}
        <div ref={bottomRef} />
      </div>
    </section>
  )
}
