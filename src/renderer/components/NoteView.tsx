import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { MessageSquare, PanelRight, X, Sparkles, Copy, Check, RefreshCw, Search, ChevronUp, ChevronDown } from 'lucide-react'
import { muesli } from '@/api'
import { Badge } from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import { SegmentedControl } from '@/components/ui/SegmentedControl'
import { cn } from '@/lib/cn'
import { countCaseInsensitiveMatches, highlightSearchText } from '@/lib/searchHighlight'
import { segmentIndexAtTime } from '@/lib/transcriptPlayback'
const NoteChatPanel = lazy(() => import('./chat/NoteChatPanel').then((m) => ({ default: m.NoteChatPanel })))
import { DiarizationReviewPanel } from './DiarizationReviewPanel'
import { Markdown } from './Markdown'
import { TranscriptView } from './TranscriptView'
import { useAnnouncer } from '@/hooks/useAnnouncer'

// The Markdown editor pulls in TipTap (the bulk of the renderer bundle); load it
// lazily so it's only fetched when the user opens the My-notes tab.
const NoteEditor = lazy(() => import('./NoteEditor').then((m) => ({ default: m.NoteEditor })))
import { isTerminal } from '../../shared/types'
import type { FullNote, SpeakerAlias, SummarySection, Template } from '../../shared/types'

type Tab = 'enhanced' | 'notes'

/** Build a Markdown document from a summary's sections. Pure + unit-testable. */
export function summaryToMarkdown(title: string, sections: SummarySection[]): string {
  if (!sections.length) return `# ${title}`
  const body = sections.map((s) => `## ${s.heading}\n\n${s.content_markdown}\n\n`).join('')
  return `# ${title}\n\n${body}`
}

function renderMarkdownLine(line: string): string | null {
  if (line.startsWith('### ')) return line.slice(4)
  if (line.startsWith('## ')) return line.slice(3)
  if (line.startsWith('# ')) return line.slice(2)
  if (line.startsWith('- ')) return line.slice(2)
  if (line.trim() === '') return null
  return line
}

function countSummaryMatches(query: string, tab: Tab, selectedSummary: FullNote['summaries'][number] | undefined): number {
  if (tab !== 'enhanced' || !selectedSummary) return 0
  const needle = query.trim()
  if (!needle) return 0

  let count = 0
  for (const section of selectedSummary.sections) {
    count += countCaseInsensitiveMatches(section.heading, needle)
    for (const line of section.content_markdown.split('\n')) {
      const rendered = renderMarkdownLine(line)
      if (rendered != null) count += countCaseInsensitiveMatches(rendered, needle)
    }
  }
  return count
}

function countTranscriptMatches(full: FullNote, query: string, showTranscript: boolean): number {
  if (!showTranscript) return 0
  const needle = query.trim()
  if (!needle) return 0
  let count = 0
  for (const seg of full.transcript?.segments ?? []) {
    count += countCaseInsensitiveMatches(seg.text, needle)
  }
  return count
}

interface SpeakerRow {
  // The RAW/original speaker label — the PUT key for upsertSpeakerAlias. Distinct
  // from `displayed` once a speaker has already been renamed once server-side
  // (the transcript's `speaker` field then shows the alias, not the raw label).
  rawLabel: string
  // The currently-displayed name, as it appears in full.transcript.segments[].speaker.
  displayed: string
}

// Computes one row per distinct displayed speaker, in order of first appearance.
// `aliases` is the fetched note_speaker_aliases list (alias_name -> speaker_label);
// used to recover the raw label when a speaker has already been renamed once.
function computeSpeakerRows(full: FullNote, aliases: SpeakerAlias[]): SpeakerRow[] {
  const seen = new Set<string>()
  const rows: SpeakerRow[] = []
  for (const seg of full.transcript?.segments ?? []) {
    const displayed = seg.speaker
    if (displayed == null || displayed === '' || seen.has(displayed)) continue
    seen.add(displayed)
    const match = aliases.find((a) => a.alias_name === displayed)
    rows.push({ rawLabel: match ? match.speaker_label : displayed, displayed })
  }
  return rows
}

function SpeakerLegend({
  full,
  overrides,
  setOverrides,
  onRenameSpeaker,
}: {
  full: FullNote
  overrides: Record<string, string>
  setOverrides: (updater: (o: Record<string, string>) => Record<string, string>) => void
  onRenameSpeaker?: (label: string, name: string) => void | Promise<void>
}) {
  const [aliases, setAliases] = useState<SpeakerAlias[]>([])
  // Gates the inputs until the alias fetch has settled (success OR failure) —
  // until then `aliases` is the placeholder `[]`, so an already-aliased
  // speaker's raw label can't yet be recovered from it. Committing a rename
  // before this resolves would key the PUT off the displayed alias name
  // instead of the true raw label, producing a dead/orphaned alias row.
  const [aliasesLoaded, setAliasesLoaded] = useState(false)
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setDrafts({})
    setError(null)
    setAliasesLoaded(false)
    let cancelled = false
    const list = muesli?.listSpeakerAliases
    if (typeof list !== 'function') {
      setAliases([])
      setAliasesLoaded(true)
      return
    }
    list(full.note.id)
      .then((result) => { if (!cancelled) setAliases(result ?? []) })
      .catch(() => { if (!cancelled) setAliases([]) })
      .finally(() => { if (!cancelled) setAliasesLoaded(true) })
    return () => { cancelled = true }
  }, [full.note.id])

  const rows = useMemo(() => computeSpeakerRows(full, aliases), [full, aliases])

  if (rows.length === 0) return null

  const commit = async (row: SpeakerRow) => {
    // Belt-and-braces alongside the disabled inputs: never fire a rename off
    // a not-yet-loaded (and therefore potentially wrong) raw-label mapping.
    if (!aliasesLoaded) return
    const draft = drafts[row.displayed]
    if (draft == null) return
    const trimmed = draft.trim()
    const current = overrides[row.displayed] ?? row.displayed
    if (!trimmed || trimmed === current) return
    try {
      await muesli.upsertSpeakerAlias(full.note.id, row.rawLabel, trimmed)
      setOverrides((o) => ({ ...o, [row.displayed]: trimmed }))
      setError(null)
      await onRenameSpeaker?.(row.rawLabel, trimmed)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not rename speaker')
    }
  }

  return (
    <div className="mb-4">
      <h3 className="mb-2 text-sm font-semibold">Speakers</h3>
      {!aliasesLoaded && (
        <p className="mb-2 text-xs text-muted-foreground">Loading speakers…</p>
      )}
      <div className="flex flex-col gap-2">
        {rows.map((row) => {
          const value = drafts[row.displayed] ?? overrides[row.displayed] ?? row.displayed
          return (
            <label key={row.displayed} className="flex flex-col gap-1 text-xs">
              <span className="text-muted-foreground">{row.displayed}</span>
              <input
                aria-label={`Rename ${row.displayed}`}
                value={value}
                disabled={!aliasesLoaded}
                aria-busy={!aliasesLoaded}
                onChange={(e) => setDrafts((d) => ({ ...d, [row.displayed]: e.target.value }))}
                onBlur={() => void commit(row)}
                onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void commit(row) } }}
                className="h-8 rounded-[var(--radius)] border border-input bg-background px-2 text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-60"
              />
            </label>
          )
        })}
      </div>
      {error && <p role="alert" className="mt-1 text-xs text-destructive">{error}</p>}
    </div>
  )
}

interface TemplateEntry {
  templateId: string
  templateName: string
  summary: FullNote['summaries'][number] | undefined
}

// The entry with the richest summary (most sections) wins as the default
// selection — matches the historical "biggest summary wins" default.
function richestTemplateId(list: TemplateEntry[]): string | null {
  if (!list.length) return null
  return list.reduce((best, e) => ((e.summary?.sections.length ?? 0) > (best.summary?.sections.length ?? 0) ? e : best)).templateId
}

// True once EVERY template-summary entry for this note is either absent or has
// zero rendered sections -- i.e. no template has ever produced a usable summary,
// as opposed to just the CURRENTLY SELECTED template lacking one (a sibling
// template on the same note may already have a real summary -- see TPL01's
// "Retro" case, which must keep the plain "No summary yet." message).
function noTemplateHasEverSummarized(entries: TemplateEntry[]): boolean {
  return entries.every((e) => (e.summary?.sections.length ?? 0) === 0)
}

export function NoteView({
  full,
  onSaveBody,
  onRenameSpeaker,
  templates,
  onRegenerateTemplate,
  regeneratingTemplateId,
  initialSegmentId,
  initialSegmentIndex,
}: {
  full: FullNote
  onSaveBody: (md: string) => Promise<void>
  onRenameSpeaker?: (label: string, name: string) => void | Promise<void>
  // ALL templates visible to the owner (built-ins + their own), fetched via
  // muesli.listTemplates() by NoteScreen. Used to list templates that don't
  // have a generated summary for this note yet, alongside ones that do.
  templates?: Template[]
  onRegenerateTemplate?: (templateId: string) => void | Promise<void>
  // The templateId currently being (re)generated, if any — disables/spinners
  // the Regenerate control so it can't be double-clicked into duplicate jobs.
  regeneratingTemplateId?: string | null
  // A transcript segment id to jump to + highlight once resolvable (e.g. a
  // NotesListScreen search hit's `?segment=` param, threaded through
  // NoteScreen). Mirrors the `jumpToCitation` flow used by summary citations.
  initialSegmentId?: string
  // A transcript segment ARRAY INDEX to jump to + highlight once resolvable,
  // e.g. a global-chat citation chip's `?segment_index=` param (ChatScreen's
  // onCiteClick), threaded through NoteScreen. Unlike `initialSegmentId`
  // this is already a 0-based index (chat.Source.segment_index mirrors
  // TranscriptRef.SegmentIndex, itself an index into the note's own
  // transcript segments — see internal/chat/retrieval.go), so no id lookup
  // is needed, just the same jumpToCitation used for summary refs.
  initialSegmentIndex?: number
}) {
  const { announce } = useAnnouncer()
  const [tab, setTab] = useState<Tab>('enhanced')
  const [showTranscript, setShowTranscript] = useState(false)
  const [showChat, setShowChat] = useState(false)
  const [findQuery, setFindQuery] = useState('')
  const [currentMatchIndex, setCurrentMatchIndex] = useState(0)
  const [transcriptAudioUrl, setTranscriptAudioUrl] = useState<string | null>(null)
  const [audioCurrentTime, setAudioCurrentTime] = useState(0)
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const noteViewRef = useRef<HTMLDivElement | null>(null)
  // 0-based transcript segment index to scroll to + highlight when a citation chip is clicked.
  const [citationTarget, setCitationTarget] = useState<number | null>(null)
  // Local speaker-rename overrides, keyed by the CURRENTLY-DISPLAYED transcript
  // speaker string (matching TranscriptView's speakerAliases contract) so a
  // rename re-labels the transcript immediately without a refetch.
  const [speakerOverrides, setSpeakerOverrides] = useState<Record<string, string>>({})
  useEffect(() => { setSpeakerOverrides({}) }, [full.note.id])

  const jumpToCitation = (ref: number) => {
    setCitationTarget(ref)
    setShowTranscript(true)
  }

  useEffect(() => {
    if (!showTranscript) {
      setTranscriptAudioUrl(null)
      setAudioCurrentTime(0)
      return
    }
    let cancelled = false
    setTranscriptAudioUrl(null)
    setAudioCurrentTime(0)
    const getAudioUrl = muesli?.getNoteAudioUrl
    if (typeof getAudioUrl !== 'function') return
    void getAudioUrl(full.note.id)
      .then((grant) => {
        if (!cancelled && grant?.url) setTranscriptAudioUrl(grant.url)
      })
      .catch(() => {
        if (!cancelled) setTranscriptAudioUrl(null)
      })
    return () => { cancelled = true }
  }, [showTranscript, full.note.id])

  // Resolve `initialSegmentId` to a transcript-segment array index and jump to
  // it exactly once per (note, segment) pair — not on every re-render. Guarded
  // by a ref (not state) so it doesn't retrigger while segments arrive
  // asynchronously (transcript may still be null on first paint).
  //
  // The "already consumed" key is (note id, segment id) COMPOUND, not just
  // the segment id/index alone: react-router does NOT remount NoteScreen/
  // NoteView when only the `:id` route param changes (see App.tsx), so this
  // same component instance -- and these refs -- persist across a navigation
  // from one note to a different one (e.g. clicking a second citation chip
  // in ChatScreen's global chat, which can point at any note). Keying the
  // ref on the marker value alone would silently suppress a legitimate jump
  // in note B whenever it happened to reuse a value already consumed in
  // note A (segment ids are globally-unique UUIDs so that's academic for
  // `initialSegmentId`, but segment_index is just a small 0-based array
  // index and collides across notes constantly -- see initialSegmentIndex
  // below, where this was an actual bug).
  const consumedInitialSegmentRef = useRef<string | null>(null)
  useEffect(() => {
    if (!initialSegmentId) return
    const key = `${full.note.id}:${initialSegmentId}`
    if (consumedInitialSegmentRef.current === key) return
    const segments = full.transcript?.segments
    if (!segments || segments.length === 0) return
    const idx = segments.findIndex((s) => s.id === initialSegmentId)
    if (idx < 0) return
    consumedInitialSegmentRef.current = key
    jumpToCitation(idx)
  }, [initialSegmentId, full.transcript, full.note.id])

  // Same one-shot-per-(note,segment) consumption as above, but for an
  // already-resolved array index (no id lookup needed). Compound-keyed on
  // (note id, index) for the reason described above.
  const consumedInitialSegmentIndexRef = useRef<string | null>(null)
  useEffect(() => {
    if (initialSegmentIndex == null || initialSegmentIndex < 0) return
    const key = `${full.note.id}:${initialSegmentIndex}`
    if (consumedInitialSegmentIndexRef.current === key) return
    consumedInitialSegmentIndexRef.current = key
    jumpToCitation(initialSegmentIndex)
  }, [initialSegmentIndex, full.note.id])

  const summaries = full.summaries
  const transcriptSegments = full.transcript?.segments ?? []
  const playingIndex = transcriptAudioUrl != null
    ? segmentIndexAtTime(transcriptSegments, audioCurrentTime * 1000)
    : null
  const seekTranscript = transcriptAudioUrl != null
    ? (startMs: number) => {
        const audio = audioRef.current
        if (!audio) return
        audio.currentTime = startMs / 1000
        setAudioCurrentTime(startMs / 1000)
      }
    : undefined

  // One entry per template visible to the owner (built-ins + their own), each
  // paired with its existing summary for this note (if any). When `templates`
  // isn't supplied (e.g. lightweight unit tests rendering NoteView directly)
  // fall back to one entry per existing summary, preserving prior behaviour.
  // NOTE: `templates` typically arrives asynchronously (NoteScreen fetches it
  // in a useEffect, so the first paint has `[]`) and its order (built-ins
  // first, then alphabetical by lowercased name — ListTemplates) can differ
  // from the summary-only fallback order (plain template-name order —
  // GetSummaries). Selection below is tracked by template IDENTITY so it
  // survives that reorder; see the reconciliation effect.
  const entries = useMemo(() => {
    if (templates && templates.length) {
      return templates.map((t) => ({
        templateId: t.id,
        templateName: t.name,
        summary: summaries.find((s) => s.template_id === t.id),
      }))
    }
    return summaries.map((s, i) => ({
      templateId: s.template_id ?? String(i),
      templateName: s.template_name,
      summary: s,
    }))
  }, [templates, summaries])

  // Selection is tracked by TEMPLATE IDENTITY (templateId), not array index/
  // position, so it survives `entries` reordering or growing — e.g. when the
  // async template list resolves after first paint and reorders relative to
  // the summary-only fallback order used before it arrived (TPL01).
  const [selectedTemplateId, setSelectedTemplateId] = useState<string | null>(() => richestTemplateId(entries))

  // Reconcile the selection whenever `entries` changes: if the previously
  // selected template is still present, keep it selected (regardless of its
  // new position); only fall back to the richest-summary default if it's no
  // longer present (or there was no prior selection).
  useEffect(() => {
    setSelectedTemplateId((prev) => {
      if (prev != null && entries.some((e) => e.templateId === prev)) return prev
      return richestTemplateId(entries)
    })
  }, [entries])

  const [pickerOpen, setPickerOpen] = useState(false)
  const pickerRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)

  const currentEntry = entries.find((e) => e.templateId === selectedTemplateId)
  const active = currentEntry?.summary
  const isRegeneratingCurrent = !!currentEntry && regeneratingTemplateId === currentEntry.templateId
  const findQueryTrimmed = findQuery.trim()
  const summaryMatchCount = useMemo(
    () => countSummaryMatches(findQueryTrimmed, tab, active),
    [active, findQueryTrimmed, full, tab],
  )
  const transcriptMatchCount = useMemo(
    () => countTranscriptMatches(full, findQueryTrimmed, showTranscript),
    [findQueryTrimmed, full, showTranscript],
  )
  const totalFindMatches = summaryMatchCount + transcriptMatchCount
  const currentFindIndex = totalFindMatches > 0 ? Math.min(currentMatchIndex, totalFindMatches - 1) : 0

  useEffect(() => {
    if (totalFindMatches === 0) {
      if (currentMatchIndex !== 0) setCurrentMatchIndex(0)
      return
    }
    if (currentMatchIndex >= totalFindMatches) {
      setCurrentMatchIndex(totalFindMatches - 1)
    }
  }, [currentMatchIndex, totalFindMatches])

  useEffect(() => {
    if (!findQueryTrimmed) return
    announce(totalFindMatches === 0 ? '0 matches' : `${currentFindIndex + 1}/${totalFindMatches}`)
  }, [announce, currentFindIndex, findQueryTrimmed, totalFindMatches])

  useEffect(() => {
    if (!findQueryTrimmed || totalFindMatches === 0) return
    const current = noteViewRef.current?.querySelector('[data-note-search-current="true"]') as HTMLElement | null
    if (!current || typeof current.scrollIntoView !== 'function') return
    current.scrollIntoView({ block: 'center', inline: 'nearest' })
  }, [currentFindIndex, findQueryTrimmed, showTranscript, tab, totalFindMatches])

  const updateFindQuery = (value: string) => {
    setFindQuery(value)
    setCurrentMatchIndex(0)
  }

  const goToNextMatch = () => {
    if (totalFindMatches === 0) return
    setCurrentMatchIndex((cur) => (cur + 1) % totalFindMatches)
  }

  const goToPrevMatch = () => {
    if (totalFindMatches === 0) return
    setCurrentMatchIndex((cur) => (cur - 1 + totalFindMatches) % totalFindMatches)
  }

  // Move focus into the picker when it opens; focus first option automatically.
  useEffect(() => {
    if (!pickerOpen) return
    const firstOption = pickerRef.current?.querySelector('[role="option"]') as HTMLElement | null
    firstOption?.focus()
  }, [pickerOpen])

  const [copied, setCopied] = useState(false)
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return () => {
      if (copyTimer.current) clearTimeout(copyTimer.current)
    }
  }, [])

  // Close the template picker on outside mousedown or Escape — same pattern as
  // FolderBar and NoteHeader overflow menu.
  useEffect(() => {
    if (!pickerOpen) return
    const onDoc = (e: MouseEvent) => {
      if (!pickerRef.current || !pickerRef.current.contains(e.target as Node)) setPickerOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { setPickerOpen(false); triggerRef.current?.focus() }
    }
    // Defer the doc listener so the opening click doesn't immediately close it.
    const t = setTimeout(() => document.addEventListener('mousedown', onDoc), 0)
    document.addEventListener('keydown', onKey)
    return () => {
      clearTimeout(t)
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [pickerOpen])

  const handleCopy = async () => {
    const title = full.note.title
    const md =
      tab === 'enhanced'
        ? summaryToMarkdown(title, active?.sections ?? [])
        : `# ${title}\n\n${full.body_markdown}`
    if (navigator.clipboard) await navigator.clipboard.writeText(md)
    setCopied(true)
    if (copyTimer.current) clearTimeout(copyTimer.current)
    copyTimer.current = setTimeout(() => setCopied(false), 1500)
  }

  let summarySearchCursor = 0
  const renderSearchableSummaryText = (text: string) => {
    const rendered = highlightSearchText(text, findQueryTrimmed, summarySearchCursor, currentFindIndex)
    summarySearchCursor += rendered.matchCount
    return rendered.nodes
  }

  return (
    <div ref={noteViewRef} className="flex gap-4">
      <div className="min-w-0 flex-1">
        <div className="mx-auto flex max-w-[72ch] items-center justify-between">
          <SegmentedControl
            ariaLabel="Note view"
            value={tab}
            onValueChange={(v) => setTab(v as Tab)}
            options={[
              { value: 'enhanced', label: 'Enhanced' },
              { value: 'notes', label: 'My notes' },
            ]}
          />
          <div className="flex items-center gap-2">
            <div className="flex items-center gap-2 rounded-[var(--radius)] border border-border bg-background px-2 py-1">
              <label className="relative block">
                <Search size={14} className="absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground" />
                <input
                  aria-label="Find in note"
                  value={findQuery}
                  onChange={(e) => updateFindQuery(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && e.shiftKey) {
                      e.preventDefault()
                      goToPrevMatch()
                    } else if (e.key === 'Enter') {
                      e.preventDefault()
                      goToNextMatch()
                    }
                  }}
                  placeholder="Find in note…"
                  className="h-7 w-40 rounded-[var(--radius)] border border-input bg-background pl-7 pr-2 text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              </label>
              <span aria-live="polite" aria-atomic="true" className="min-w-16 text-center text-xs text-muted-foreground">
                {totalFindMatches === 0 ? '0 matches' : `${currentFindIndex + 1}/${totalFindMatches}`}
              </span>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                className="h-7 px-2 text-xs"
                aria-label="Previous match"
                disabled={totalFindMatches === 0}
                onClick={goToPrevMatch}
              >
                <ChevronUp size={14} />
              </Button>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                className="h-7 px-2 text-xs"
                aria-label="Next match"
                disabled={totalFindMatches === 0}
                onClick={goToNextMatch}
              >
                <ChevronDown size={14} />
              </Button>
            </div>
            {tab === 'enhanced' && entries.length > 1 && (
              <div className="relative" ref={pickerRef}>
                <button
                  ref={triggerRef}
                  type="button"
                  aria-label="Switch template"
                  aria-haspopup="listbox"
                  aria-expanded={pickerOpen}
                  onClick={() => setPickerOpen((o) => !o)}
                  className="inline-flex items-center gap-1 rounded-[var(--radius)] px-3 py-1 text-sm font-medium text-muted-foreground hover:bg-muted"
                >
                  <Sparkles size={16} /> {currentEntry?.templateName ?? 'Template'}
                </button>
                {pickerOpen && (
                  <div role="listbox" aria-label="Select template" className="absolute right-0 z-10 mt-1 w-56 rounded-[var(--radius)] border border-border bg-popover p-1 shadow-md">
                    {entries.map((e) => (
                      <button
                        key={e.templateId}
                        role="option"
                        aria-selected={e.templateId === selectedTemplateId}
                        onClick={() => { setSelectedTemplateId(e.templateId); setPickerOpen(false); triggerRef.current?.focus() }}
                        className={cn('block w-full truncate rounded px-2 py-1 text-left text-sm hover:bg-muted', e.templateId === selectedTemplateId && 'text-primary')}
                      >
                        {e.templateName}
                        {!e.summary && <span className="text-muted-foreground"> · Not generated</span>}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}
            {tab === 'enhanced' && onRegenerateTemplate && currentEntry && (
              <button
                type="button"
                aria-label="Regenerate summary"
                aria-busy={isRegeneratingCurrent}
                disabled={isRegeneratingCurrent}
                onClick={() => onRegenerateTemplate(currentEntry.templateId)}
                className="inline-flex items-center gap-1 rounded-[var(--radius)] px-3 py-1 text-sm font-medium text-muted-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-60"
              >
                <RefreshCw size={16} className={cn(isRegeneratingCurrent && 'animate-spin')} /> Regenerate
              </button>
            )}
            <button
              type="button"
              aria-label="Copy as Markdown"
              onClick={handleCopy}
              className="inline-flex items-center gap-1 rounded-[var(--radius)] px-3 py-1 text-sm font-medium text-muted-foreground hover:bg-muted"
            >
              {copied ? <Check size={16} /> : <Copy size={16} />} {copied ? 'Copied' : 'Copy'}
            </button>
            <DiarizationReviewPanel
              noteId={full.note.id}
              hasTranscript={(full.transcript?.segments.length ?? 0) > 0}
            />
            <button
              type="button"
              aria-label="Transcript"
              aria-pressed={showTranscript}
              onClick={() => setShowTranscript((s) => !s)}
              className={cn(
                'inline-flex items-center gap-1 rounded-[var(--radius)] px-3 py-1 text-sm font-medium',
                showTranscript ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-muted',
              )}
            >
              <PanelRight size={16} /> Transcript
            </button>
            <button
              type="button"
              aria-label="Ask about this note"
              aria-pressed={showChat}
              onClick={() => setShowChat((s) => !s)}
              className={cn(
                'inline-flex items-center gap-1 rounded-[var(--radius)] px-3 py-1 text-sm font-medium',
                showChat ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-muted',
              )}
            >
              <MessageSquare size={16} /> Chat
            </button>
          </div>
        </div>

        <div className="mx-auto mt-4 max-w-[72ch]">
          {tab === 'enhanced' &&
            (active && active.sections.length > 0 ? (
              <>
                {active.truncated && (
                  <Badge tone="accent" className="mb-3">
                    May be truncated
                  </Badge>
                )}
                {active.sections.map((sec, i) => (
                  <section key={i} className="mb-6">
                    <h3 className="mb-1 text-sm font-semibold uppercase tracking-wide text-muted-foreground">{renderSearchableSummaryText(sec.heading)}</h3>
                    <Markdown source={sec.content_markdown} renderText={renderSearchableSummaryText} />
                    {sec.refs && sec.refs.length > 0 && (
                      <div className="mt-2 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                        <span>Sources:</span>
                        {sec.refs.map((ref) => (
                          <button
                            key={ref}
                            type="button"
                            aria-label={`Jump to transcript segment ${ref + 1}`}
                            onClick={() => jumpToCitation(ref)}
                            className="inline-flex min-w-[1.5rem] items-center justify-center rounded-[var(--radius)] border border-border px-1.5 py-0.5 font-mono tabular-nums text-muted-foreground hover:bg-primary/10 hover:text-primary"
                          >
                            {ref + 1}
                          </button>
                        ))}
                      </div>
                    )}
                  </section>
                ))}
              </>
            ) : isTerminal(full.note.status) && transcriptSegments.length > 0 && noTemplateHasEverSummarized(entries) ? (
              <div className="max-w-md text-sm text-muted-foreground">
                <p className="mb-3">No AI summary is available for this note -- no summarization agent is configured (e.g. Ollama isn&apos;t installed).</p>
                <p className="mb-3">Your recorded transcript is still available.</p>
                <Button type="button" variant="secondary" size="sm" onClick={() => setShowTranscript(true)}>
                  View transcript
                </Button>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">No summary yet.</p>
            ))}
          {tab === 'notes' && (
            <Suspense fallback={<div className="p-4 text-sm text-muted-foreground">Loading editor…</div>}>
              <NoteEditor initialMarkdown={full.body_markdown} onSave={onSaveBody} />
            </Suspense>
          )}
        </div>
      </div>

      {showTranscript && (
        <aside className="w-[20rem] shrink-0 border-l border-border pl-4">
          <div className="mb-2 flex items-center justify-between">
            <h3 className="text-sm font-semibold">Transcript</h3>
            <button
              type="button"
              aria-label="Close transcript"
              onClick={() => setShowTranscript(false)}
              className="text-muted-foreground hover:text-foreground"
            >
              <X size={16} />
            </button>
          </div>
          {transcriptAudioUrl && (
            // No captions track exists for recorded meeting audio; this is a playback control only.
            // eslint-disable-next-line jsx-a11y/media-has-caption
            <audio
              ref={audioRef}
              controls
              src={transcriptAudioUrl}
              className="mb-3 w-full"
              onTimeUpdate={(e) => setAudioCurrentTime(e.currentTarget.currentTime)}
              onLoadedMetadata={(e) => setAudioCurrentTime(e.currentTarget.currentTime)}
            />
          )}
          <SpeakerLegend
            full={full}
            overrides={speakerOverrides}
            setOverrides={setSpeakerOverrides}
            onRenameSpeaker={onRenameSpeaker}
          />
          <TranscriptView
            segments={transcriptSegments}
            highlightIndex={citationTarget}
            onSeek={seekTranscript}
            playingIndex={playingIndex}
            speakerAliases={speakerOverrides}
            searchQuery={showTranscript ? findQueryTrimmed : undefined}
            searchCurrentIndex={showTranscript ? currentFindIndex : null}
            searchStartIndex={summaryMatchCount}
            hideSearchInput
          />
        </aside>
      )}

      {showChat && (
        <aside className="w-[22rem] shrink-0 border-l border-border pl-4">
          <Suspense fallback={<div className="pt-1 text-sm text-muted-foreground">Loading chat…</div>}>
            <NoteChatPanel
              noteId={full.note.id}
              onClose={() => setShowChat(false)}
              onCiteClick={(source) => jumpToCitation(source.segment_index)}
            />
          </Suspense>
        </aside>
      )}
    </div>
  )
}
