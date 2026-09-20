import { Fragment } from 'react'
import { cn } from '@/lib/cn'
import { Markdown } from '../Markdown'
import type { ChatSource, Message, MessageSource } from '../../../shared/types'

// Splits a line of assistant text on `[n]` citation markers, keeping the
// delimiters, so each marker can be rendered independently of the
// surrounding plain text (which is returned unchanged).
const CITATION_MARKER_RE = /(\[\d+\])/g
const CITATION_MARKER_EXACT_RE = /^\[(\d+)\]$/

// Renders one line of assistant text with inline citation chips: a `[n]`
// marker that matches a source (by `n`) becomes a small clickable chip
// (native `title` tooltip = snippet, click = onCite); a `[n]` marker with NO
// matching source (hallucinated / duplicate / out-of-range -- ParseCitations
// silently drops those server-side) degrades to inert literal text, per the
// citation contract. Never throws on malformed/missing markers.
// CiteableSource is the shape renderTextWithCitations needs, satisfied
// structurally by both ChatSource (ordinary chat, note_id always present)
// and model.MessageSource (cross-meeting analysis, note_id nullable when its
// note has since been deleted -- see the accepted spec's "Persistence"
// section).
interface CiteableSource {
  n: number
  note_id: string | null
  snippet: string
}

// Renders one line of assistant text with inline citation chips: a `[n]`
// marker that matches a source (by `n`) becomes a small chip (native
// `title` tooltip = snippet). When the matching source's note_id is a real
// string, the chip is clickable and calls onCite; when note_id is null (the
// cited note was deleted -- issue #765), the chip renders INERT: still
// visible with its snippet, but not a button and never calls onCite. A `[n]`
// marker with NO matching source (hallucinated / duplicate / out-of-range)
// degrades to inert literal text, per the citation contract. Never throws on
// malformed/missing markers.
function renderTextWithCitations<S extends CiteableSource>(
  text: string,
  sourcesByN: Map<number, S>,
  onCite: (source: S & { note_id: string }) => void,
): React.ReactNode {
  if (!text.includes('[')) return text
  const parts = text.split(CITATION_MARKER_RE)
  if (parts.length === 1) return text
  return parts.map((part, i) => {
    const match = CITATION_MARKER_EXACT_RE.exec(part)
    if (!match) return <Fragment key={i}>{part}</Fragment>
    const source = sourcesByN.get(Number(match[1]))
    if (!source) return <Fragment key={i}>{part}</Fragment>
    if (source.note_id === null) {
      return (
        <span
          key={i}
          aria-label={`Citation ${source.n}: unavailable (note deleted)`}
          title="This meeting was deleted; the citation is no longer available."
          className="mx-0.5 inline-flex min-w-[1.25rem] items-center justify-center rounded-[var(--radius)] border border-border px-1 font-mono text-xs tabular-nums text-muted-foreground/50"
        >
          {part}
        </span>
      )
    }
    return (
      <button
        key={i}
        type="button"
        aria-label={`Citation ${source.n}: ${source.snippet}`}
        title={source.snippet}
        onClick={() => onCite(source as S & { note_id: string })}
        className="mx-0.5 inline-flex min-w-[1.25rem] items-center justify-center rounded-[var(--radius)] border border-border px-1 font-mono text-xs tabular-nums text-muted-foreground hover:bg-primary/10 hover:text-primary"
      >
        {part}
      </button>
    )
  })
}

// Role-differentiated message thread. Assistant content renders through the
// existing Markdown component (no new markdown dependency); citation markers
// ([1], [2], …) already appear inline in the assistant's plain-text content
// and are rendered as clickable chips in place, using the matching entry
// from that message's `sources` (matched by `n`) when one exists.
export function ChatThread({
  messages,
  sourcesByMessageId,
  loading,
  emptyLabel,
  onCiteClick,
  onCiteMessageSourceClick,
}: {
  messages: Message[]
  sourcesByMessageId: Record<string, ChatSource[]>
  loading?: boolean
  emptyLabel?: string
  // Invoked when an ORDINARY chat citation chip is clicked, with the
  // ChatSource it resolved to (sourcesByMessageId, keyed by the send/create
  // response). Callers (NoteChatPanel / ChatScreen) decide how to
  // navigate/jump -- ChatThread itself has no notion of routing.
  onCiteClick?: (source: ChatSource) => void
  // Invoked when a CROSS-MEETING ANALYSIS citation chip is clicked (issue
  // #765), with the model.MessageSource it resolved to -- distinct from
  // onCiteClick because MessageSource carries different fields (a
  // transcript_generation, a nullable note_id already filtered to non-null
  // here).
  onCiteMessageSourceClick?: (source: MessageSource & { note_id: string }) => void
}) {
  return (
    <div role="log" aria-label="Conversation" aria-busy={!!loading} className="flex-1 overflow-y-auto">
      {loading && <p className="text-sm text-muted-foreground">Loading messages…</p>}
      {!loading && messages.length === 0 && emptyLabel && (
        <p className="text-sm text-muted-foreground">{emptyLabel}</p>
      )}
      {messages.map((m) => {
        const isUser = m.role === 'user'
        // A cross-meeting analysis assistant message carries its own
        // sources inline (model.Message.Sources, populated after reload
        // too); ordinary chat's citations arrive out-of-band via
        // sourcesByMessageId (only for the turn just sent, not after
        // reload). A message never has both.
        const crossSources = m.sources
        const chatSources = sourcesByMessageId[m.id]
        return (
          <div
            key={m.id}
            data-role={m.role}
            className={cn(
              'mb-3 max-w-[85%] rounded-[var(--radius)] px-3 py-2 text-sm',
              isUser ? 'ml-auto bg-primary/10' : 'mr-auto bg-muted',
            )}
          >
            <div className="mb-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              {isUser ? 'You' : 'Assistant'}
            </div>
            {isUser ? (
              <p className="whitespace-pre-wrap">{m.content}</p>
            ) : crossSources && crossSources.length > 0 ? (
              <Markdown
                source={m.content}
                renderText={(text) =>
                  renderTextWithCitations(text, new Map(crossSources.map((s) => [s.n, s])), (s) => onCiteMessageSourceClick?.(s))
                }
              />
            ) : (
              <Markdown
                source={m.content}
                renderText={(text) =>
                  renderTextWithCitations(text, new Map((chatSources ?? []).map((s) => [s.n, s])), (s) => onCiteClick?.(s))
                }
              />
            )}
          </div>
        )
      })}
    </div>
  )
}
