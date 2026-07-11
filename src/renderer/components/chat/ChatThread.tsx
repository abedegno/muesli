import { Fragment } from 'react'
import { cn } from '@/lib/cn'
import { Markdown } from '../Markdown'
import type { ChatSource, Message } from '../../../shared/types'

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
function renderTextWithCitations(
  text: string,
  sourcesByN: Map<number, ChatSource>,
  onCite: (source: ChatSource) => void,
): React.ReactNode {
  if (!text.includes('[')) return text
  const parts = text.split(CITATION_MARKER_RE)
  if (parts.length === 1) return text
  return parts.map((part, i) => {
    const match = CITATION_MARKER_EXACT_RE.exec(part)
    if (!match) return <Fragment key={i}>{part}</Fragment>
    const source = sourcesByN.get(Number(match[1]))
    if (!source) return <Fragment key={i}>{part}</Fragment>
    return (
      <button
        key={i}
        type="button"
        aria-label={`Citation ${source.n}: ${source.snippet}`}
        title={source.snippet}
        onClick={() => onCite(source)}
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
}: {
  messages: Message[]
  sourcesByMessageId: Record<string, ChatSource[]>
  loading?: boolean
  emptyLabel?: string
  // Invoked when a citation chip is clicked, with the source it resolved to.
  // Callers (NoteChatPanel / ChatScreen) decide how to navigate/jump —
  // ChatThread itself has no notion of routing or the current note.
  onCiteClick?: (source: ChatSource) => void
}) {
  return (
    <div role="log" aria-label="Conversation" aria-busy={!!loading} className="flex-1 overflow-y-auto">
      {loading && <p className="text-sm text-muted-foreground">Loading messages…</p>}
      {!loading && messages.length === 0 && emptyLabel && (
        <p className="text-sm text-muted-foreground">{emptyLabel}</p>
      )}
      {messages.map((m) => {
        const sources = sourcesByMessageId[m.id]
        const sourcesByN = new Map((sources ?? []).map((s) => [s.n, s]))
        const isUser = m.role === 'user'
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
            ) : (
              <Markdown
                source={m.content}
                renderText={(text) => renderTextWithCitations(text, sourcesByN, (s) => onCiteClick?.(s))}
              />
            )}
          </div>
        )
      })}
    </div>
  )
}
