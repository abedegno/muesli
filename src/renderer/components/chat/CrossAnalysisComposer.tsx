import { useMemo, useState } from 'react'
import { CROSS_ANALYSIS_MAX_NOTES, CROSS_ANALYSIS_MIN_NOTES, isReady, type Note, type Template } from '../../../shared/types'

// CrossAnalysisComposer is the composer for issue #765's cross-meeting
// analysis mode, shown only in global Chat (never note-scoped chat). It
// lets the user pick one owner-visible cross-phase template, a
// searchable/filterable multi-select of eligible completed notes, and an
// optional run-specific focus, then submit an explicit ordered note-id list.
//
// Filters are selection aids, not persistent predicates (the accepted
// spec): "Select all" resolves against the CURRENTLY DISPLAYED filtered
// result only, and the actual submitted list is always the fully-resolved
// explicit set of note ids at submit time -- never a filter expression.
export function CrossAnalysisComposer({
  templates,
  notes,
  sending,
  onSend,
}: {
  templates: Template[]
  notes: Note[]
  sending: boolean
  onSend: (templateId: string, noteIds: string[], focus: string) => Promise<boolean>
}) {
  const [templateId, setTemplateId] = useState('')
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [focus, setFocus] = useState('')

  // Owner-visible templates whose phase is cross -- never other phases.
  const crossTemplates = useMemo(() => templates.filter((t) => t.phase === 'cross'), [templates])

  // Eligible notes: ready, not partial. Hidden entirely from the picker --
  // never shown greyed-out -- an ineligible note simply cannot be selected.
  const eligibleNotes = useMemo(() => notes.filter((n) => isReady(n) && !n.partial_transcript), [notes])

  const filteredNotes = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return eligibleNotes
    return eligibleNotes.filter((n) => n.title.toLowerCase().includes(q))
  }, [eligibleNotes, query])

  const selectedCount = selected.size
  const canRun = !sending && templateId !== '' && selectedCount >= CROSS_ANALYSIS_MIN_NOTES && selectedCount <= CROSS_ANALYSIS_MAX_NOTES

  const toggleNote = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else if (next.size < CROSS_ANALYSIS_MAX_NOTES) {
        next.add(id)
      }
      return next
    })
  }

  // Selects every note in the CURRENTLY DISPLAYED (filtered) result, bounded
  // at the shared maximum -- filters never become part of the submitted
  // request, only this one-time resolution to explicit ids.
  const selectAllFiltered = () => {
    setSelected((prev) => {
      const next = new Set(prev)
      for (const n of filteredNotes) {
        if (next.size >= CROSS_ANALYSIS_MAX_NOTES) break
        next.add(n.id)
      }
      return next
    })
  }

  const clearSelection = () => setSelected(new Set())

  const submit = async () => {
    if (!canRun) return
    // Resolve selection to an explicit, ordered, duplicate-free id list
    // (Set iteration order is insertion order) before submission.
    const noteIds = Array.from(selected)
    const ok = await onSend(templateId, noteIds, focus.trim())
    if (ok) {
      setSelected(new Set())
      setFocus('')
    }
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
      className="mt-2 flex flex-col gap-2"
      aria-label="Cross-meeting analysis"
    >
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-xs font-medium text-muted-foreground">Template</span>
        <select
          aria-label="Cross-analysis template"
          value={templateId}
          disabled={sending}
          onChange={(e) => setTemplateId(e.target.value)}
          className="rounded-[var(--radius)] border border-input bg-background px-2 py-1.5 text-sm disabled:cursor-not-allowed disabled:opacity-60"
        >
          <option value="">Select a template…</option>
          {crossTemplates.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name}
            </option>
          ))}
        </select>
      </label>

      <div className="flex flex-col gap-1">
        <div className="flex items-center justify-between">
          <span className="text-xs font-medium text-muted-foreground">
            Meetings ({selectedCount}/{CROSS_ANALYSIS_MAX_NOTES})
          </span>
          <div className="flex gap-2">
            <button type="button" onClick={selectAllFiltered} disabled={sending} className="text-xs text-primary hover:underline disabled:cursor-not-allowed disabled:opacity-60">
              Select all
            </button>
            <button type="button" onClick={clearSelection} disabled={sending} className="text-xs text-muted-foreground hover:underline disabled:cursor-not-allowed disabled:opacity-60">
              Clear
            </button>
          </div>
        </div>
        <input
          aria-label="Filter meetings"
          type="text"
          value={query}
          disabled={sending}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filter meetings by title…"
          className="rounded-[var(--radius)] border border-input bg-background px-2 py-1.5 text-sm disabled:cursor-not-allowed disabled:opacity-60"
        />
        <ul role="listbox" aria-label="Eligible meetings" aria-multiselectable="true" className="max-h-40 overflow-y-auto rounded-[var(--radius)] border border-input">
          {filteredNotes.length === 0 && <li className="px-2 py-1.5 text-xs text-muted-foreground">No eligible meetings match.</li>}
          {filteredNotes.map((n) => {
            const isSelected = selected.has(n.id)
            return (
              <li key={n.id}>
                <label className="flex cursor-pointer items-center gap-2 px-2 py-1.5 text-sm hover:bg-muted">
                  <input
                    type="checkbox"
                    checked={isSelected}
                    disabled={sending || (!isSelected && selectedCount >= CROSS_ANALYSIS_MAX_NOTES)}
                    onChange={() => toggleNote(n.id)}
                  />
                  <span>{n.title}</span>
                </label>
              </li>
            )
          })}
        </ul>
      </div>

      <label className="flex flex-col gap-1 text-sm">
        <span className="text-xs font-medium text-muted-foreground">Focus (optional)</span>
        <textarea
          aria-label="Cross-analysis focus"
          value={focus}
          disabled={sending}
          onChange={(e) => setFocus(e.target.value)}
          rows={2}
          placeholder="Compare decisions and unresolved risks…"
          className="resize-none rounded-[var(--radius)] border border-input bg-background px-2 py-1.5 text-sm disabled:cursor-not-allowed disabled:opacity-60"
        />
      </label>

      <button
        type="submit"
        disabled={!canRun}
        aria-busy={sending}
        className="h-9 shrink-0 self-start rounded-[var(--radius)] bg-primary px-3 text-sm font-medium text-primary-foreground disabled:cursor-not-allowed disabled:opacity-60"
      >
        {sending ? 'Running analysis…' : 'Run analysis'}
      </button>
    </form>
  )
}
