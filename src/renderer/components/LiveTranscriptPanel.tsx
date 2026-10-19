import { uiText } from '@/i18n'
import { useEffect, useRef, useState } from 'react'
import { muesli } from '@/api'
import { t } from '@/i18n'
import type { NoteStreamEvent } from '../../shared/ipc'

type LiveState = 'idle' | 'connecting' | 'loading' | 'live' | 'unavailable' | 'dropped'

interface LiveTranscriptSource {
  onNoteStreamEvent?: (cb: (event: NoteStreamEvent) => void) => () => void
}

export function LiveTranscriptPanel({
  noteId,
  isRecording,
  source = muesli,
  hasFinalTranscript = false,
}: {
  noteId: string
  isRecording: boolean
  source?: LiveTranscriptSource
  hasFinalTranscript?: boolean
}) {
  const [status, setStatus] = useState<LiveState>('idle')
  const [segments, setSegments] = useState<Extract<NoteStreamEvent, { type: 'segment' }>[]>([])
  const [interimSegment, setInterimSegment] = useState<{
    text: string
    start_ms: number
    end_ms: number
    speaker: string | null
  } | null>(null)
  const wordsRef = useRef<HTMLDivElement | null>(null)
  const followRef = useRef(true)
  const [collapsed, setCollapsed] = useState(
    () => typeof window !== 'undefined' && window.innerWidth < 700
  )

  useEffect(() => {
    setStatus('idle')
    setSegments([])
    setInterimSegment(null)
  }, [noteId])

  useEffect(() => {
    if (isRecording) {
      setStatus('connecting')
      followRef.current = true
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
        setSegments((current) => [...current, event].slice(-2000))
        setInterimSegment(null)
        return
      }
      if (event.type === 'connecting') {
        setStatus('connecting')
        setSegments([])
        setInterimSegment(null)
        return
      }
      setStatus(event.type)
    })
    return unsubscribe
  }, [noteId, source])

  useEffect(() => {
    if (status !== 'live' || collapsed || !followRef.current) return
    const words = wordsRef.current
    words?.scrollTo?.({ top: words.scrollHeight, behavior: 'smooth' })
  }, [interimSegment, segments, status, collapsed])

  const hasWords = segments.length > 0 || interimSegment !== null
  if (!isRecording && (!hasWords || hasFinalTranscript)) return null

  if ((status === 'unavailable' || status === 'dropped') && !hasWords) {
    return (
      <div
        data-testid="live-transcript-unavailable"
        className="mx-6 rounded-[var(--radius)] border border-border/60 bg-muted/30 px-4 py-3 text-sm text-muted-foreground"
        role="status"
        aria-live="polite"
      >
        {uiText('Live transcript unavailable — the full transcript will be ready after recording.')}
      </div>
    )
  }

  if (status === 'loading' && !hasWords) {
    return (
      <section
        data-testid="live-transcript-panel"
        className="mx-6 rounded-[var(--radius)] border border-amber-500/40 bg-amber-500/5 px-4 py-3 shadow-sm"
        role="status"
        aria-live="polite"
      >
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <span
            className="inline-flex h-2 w-2 animate-pulse rounded-full bg-amber-500"
            aria-hidden
          />
          <span className="font-medium text-foreground">{uiText('Live transcript')}</span>
          <span aria-hidden>{uiText('·')}</span>
          <span>{uiText('Starting')}</span>
        </div>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">
          {uiText('Starting the transcription engine — this takes a few seconds the first time.')}
        </p>
      </section>
    )
  }

  return (
    <section
      data-testid="live-transcript-panel"
      className="mx-6 rounded-[var(--radius)] border border-border/70 bg-background/80 px-4 py-3 shadow-sm"
    >
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <span
          className={`inline-flex h-2 w-2 rounded-full ${isRecording && status === 'live' ? 'bg-emerald-500' : 'bg-muted-foreground'}`}
          aria-hidden
        />
        <span className="font-medium text-foreground">{uiText('Live transcript')}</span>
        <span aria-hidden>{uiText('·')}</span>
        <span>
          {!isRecording
            ? t('live.stopped')
            : status === 'connecting'
              ? uiText('Connecting…')
              : uiText('Live')}
        </span>
      </div>
      {!isRecording && (
        <p className="mt-2 text-sm text-muted-foreground">{t('live.previewHelp')}</p>
      )}
      {isRecording && (status === 'dropped' || status === 'unavailable') && (
        <p role="status" className="mt-2 text-sm text-muted-foreground">
          {t('live.disconnected')}
        </p>
      )}
      <button
        type="button"
        className="mt-2 text-xs text-muted-foreground underline"
        aria-expanded={!collapsed}
        aria-controls={`live-words-${noteId}`}
        onClick={() => setCollapsed((value) => !value)}
      >
        {collapsed ? uiText('Show live text') : uiText('Hide live text')}
      </button>
      <div
        id={`live-words-${noteId}`}
        ref={wordsRef}
        hidden={collapsed}
        role="region"
        aria-label={uiText('Live transcript preview')}
        // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- Keyboard access to this scroll region or separator.
        tabIndex={0}
        onScroll={(event) => {
          const node = event.currentTarget
          followRef.current = node.scrollHeight - node.scrollTop - node.clientHeight < 40
        }}
        className="mt-3 max-h-48 space-y-3 overflow-y-auto text-sm leading-6 text-foreground"
      >
        {segments.length === 0 && interimSegment === null ? (
          <p className="text-muted-foreground">{uiText('Listening…')}</p>
        ) : (
          <>
            {segments.map((segment) => (
              <p
                key={`${segment.start_ms}-${segment.end_ms}-${segment.text}`}
                className="whitespace-pre-wrap"
              >
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
      </div>
    </section>
  )
}
