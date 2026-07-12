import { useEffect, useRef, useState } from 'react'
import { muesli } from '@/api'
import type { NoteStreamEvent } from '../../shared/ipc'

type LiveState = 'idle' | 'connecting' | 'live' | 'unavailable' | 'dropped'

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
  const bottomRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!isRecording) {
      setStatus('idle')
      setSegments([])
    } else {
      setStatus('connecting')
      setSegments([])
    }
  }, [isRecording])

  useEffect(() => {
    if (typeof source.onNoteStreamEvent !== 'function') return
    const unsubscribe = source.onNoteStreamEvent((event) => {
      if (event.noteId !== noteId) return
      if (event.type === 'segment') {
        setStatus('live')
        setSegments((current) => [...current, event])
        return
      }
      if (event.type === 'connecting') {
        setStatus('connecting')
        setSegments([])
        return
      }
      setStatus(event.type)
      setSegments([])
    })
    return unsubscribe
  }, [noteId, source])

  useEffect(() => {
    if (status !== 'live') return
    bottomRef.current?.scrollIntoView({ block: 'end', behavior: 'smooth' })
  }, [segments, status])

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

  return (
    <section data-testid="live-transcript-panel" className="mx-6 rounded-[var(--radius)] border border-border/70 bg-background/80 px-4 py-3 shadow-sm">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <span className="inline-flex h-2 w-2 rounded-full bg-emerald-500" aria-hidden />
        <span className="font-medium text-foreground">Live transcript</span>
        <span aria-hidden>·</span>
        <span>Live</span>
      </div>
      <div className="mt-3 space-y-3 text-sm leading-6 text-foreground">
        {segments.length === 0 ? (
          <p className="text-muted-foreground">Listening for the first finalized segment…</p>
        ) : (
          segments.map((segment) => (
            <p key={`${segment.start_ms}-${segment.end_ms}-${segment.text}`} className="whitespace-pre-wrap">
              {segment.text}
            </p>
          ))
        )}
        <div ref={bottomRef} />
      </div>
    </section>
  )
}
