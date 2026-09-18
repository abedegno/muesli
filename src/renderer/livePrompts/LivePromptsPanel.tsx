import { useEffect, useMemo, useRef, useState } from 'react'
import { Markdown } from '../components/Markdown'
import { subscribeLivePrompts, type LivePromptsEvent, type LivePromptsItem } from './transport'

interface LivePromptsSource {
  subscribe?: (noteId: string, callback: (event: LivePromptsEvent) => void) => () => void
}

function itemKey(item: Pick<LivePromptsItem, 'stream_id' | 'template_id'>): string {
  return `${item.stream_id}:${item.template_id}`
}

/**
 * Renders inline `[n]` citation markers as inert (non-navigating) text.
 * Live references resolve only against the matching current transcript
 * prefix and must never enter the note's batch citation-click handling
 * after the stream ends (see the accepted spec) -- rendering them as plain
 * text rather than a clickable chip makes that impossible by construction,
 * at the cost of click-to-scroll navigation for now.
 */
function renderLiveCitations(text: string): React.ReactNode {
  const parts = text.split(/(\[\d+\])/g)
  if (parts.length === 1) return text
  return parts.map((part, i) => (
    <span key={i} className={/^\[\d+\]$/.test(part) ? 'text-muted-foreground/70' : undefined}>
      {part}
    </span>
  ))
}

function LivePromptCard({ item, isUpdating }: { item: LivePromptsItem; isUpdating: boolean }) {
  const hasContent = item.sections.length > 0

  let footer: React.ReactNode = null
  if (item.status === 'running' && hasContent) footer = 'Updating.'
  else if (item.status === 'failed' && hasContent) footer = 'Not updated.'

  return (
    <div
      data-testid={`live-prompt-card-${item.template_id}`}
      className="rounded-[var(--radius)] border border-border/70 bg-background/80 px-4 py-3 shadow-sm"
    >
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <span className="font-medium text-foreground">{item.template_name}</span>
        {isUpdating ? <span aria-hidden>&middot;</span> : null}
        {isUpdating ? <span>Updating</span> : null}
      </div>
      <div className="mt-2 text-sm leading-6 text-foreground">
        {!hasContent && item.status !== 'failed' ? (
          <p className="text-muted-foreground" data-testid={`live-prompt-waiting-${item.template_id}`}>
            {item.status === 'running' ? 'Generating…' : 'Waiting for speech…'}
          </p>
        ) : null}
        {!hasContent && item.status === 'failed' ? (
          <p className="text-muted-foreground" data-testid={`live-prompt-failed-${item.template_id}`}>
            It will retry when the transcript grows.
          </p>
        ) : null}
        {hasContent
          ? item.sections.map((section, i) => (
              <div key={i} className="mt-2 first:mt-0">
                <Markdown source={section.content_markdown} renderText={renderLiveCitations} />
              </div>
            ))
          : null}
      </div>
      {footer ? (
        <p className="mt-2 text-xs text-muted-foreground" data-testid={`live-prompt-footer-${item.template_id}`}>
          {footer}
        </p>
      ) : null}
    </div>
  )
}

/**
 * "Live prompts": one card per participating during-phase template, shown
 * adjacent to the live transcript only while recording (issue #764). Cards
 * are keyed by (stream_id,template_id) in the server's stable order;
 * `ended` removes a card immediately, a `snapshot` (initial connect or
 * reconnect) atomically replaces the whole set, and an `update` replaces
 * one card's content atomically without disturbing the others' order.
 */
export function LivePromptsPanel({
  noteId,
  isRecording,
  subscribe = subscribeLivePrompts,
}: {
  noteId: string
  isRecording: boolean
  subscribe?: LivePromptsSource['subscribe']
}) {
  const [order, setOrder] = useState<string[]>([])
  const [items, setItems] = useState<Record<string, LivePromptsItem>>({})
  const [announcement, setAnnouncement] = useState('')
  const itemsRef = useRef(items)
  itemsRef.current = items

  useEffect(() => {
    setOrder([])
    setItems({})
    setAnnouncement('')
    if (!isRecording) return
    const unsubscribe = subscribe(noteId, (event) => {
      if (event.type === 'snapshot') {
        const nextOrder = event.items.map(itemKey)
        const nextItems: Record<string, LivePromptsItem> = {}
        for (const item of event.items) nextItems[itemKey(item)] = item
        setOrder(nextOrder)
        setItems(nextItems)
        return
      }
      if (event.type === 'update') {
        const key = itemKey(event.item)
        const prior = itemsRef.current[key]
        if (prior && prior.status !== 'ready' && event.item.status === 'ready') {
          setAnnouncement(`${event.item.template_name} updated.`)
        }
        setItems((current) => ({ ...current, [key]: event.item }))
        setOrder((current) => (current.includes(key) ? current : [...current, key]))
        return
      }
      if (event.type === 'ended') {
        const key = itemKey({ stream_id: event.streamId, template_id: event.templateId })
        setOrder((current) => current.filter((k) => k !== key))
        setItems((current) => {
          const next = { ...current }
          delete next[key]
          return next
        })
      }
    })
    return unsubscribe
  }, [noteId, isRecording, subscribe])

  const orderedItems = useMemo(() => order.map((key) => items[key]).filter(Boolean), [order, items])

  if (!isRecording || orderedItems.length === 0) return null

  return (
    <section data-testid="live-prompts-panel" className="mx-6 flex flex-col gap-3">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <span className="inline-flex h-2 w-2 rounded-full bg-emerald-500" aria-hidden />
        <span className="font-medium text-foreground">Live prompts</span>
      </div>
      {orderedItems.map((item) => (
        <LivePromptCard key={itemKey(item)} item={item} isUpdating={item.status === 'running' && item.sections.length > 0} />
      ))}
      <div role="status" aria-live="polite" className="sr-only">
        {announcement}
      </div>
    </section>
  )
}
