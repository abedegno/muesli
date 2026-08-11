import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { MoreHorizontal, Pin } from 'lucide-react'
import { muesli } from '@/api'
import { RecordControl, type RecordState } from './RecordControl'
import { NoteEventLink } from './NoteEventLink'
import { Dialog } from './ui/Dialog'
import { Button } from './ui/Button'
import { useAnnouncer } from '@/hooks/useAnnouncer'
import type { RetranscribeNoteRequest } from '../../shared/types'

type ExportFormatAction = {
  id: string
  label: string
  onSelect: () => void
}

export function NoteHeader({
  noteId,
  title,
  recordState,
  elapsedMs,
  micError,
  onStart,
  onStop,
  onDeviceChange,
  onGainChange,
  recordingLevel,
  onMicRetry,
  onTitleSaved,
  onDeleteNote,
  onDuplicate,
  onExport,
  pinned,
  onTogglePinned,
  eventId,
  onLinkEvent,
  onUnlinkEvent,
  onExportPlainText,
  onExportSrt,
  onExportAss,
  onExportVtt,
  onExportWithOptions,
  serverExportFormats,
  onResummarize,
  onRetranscribe,
  autoFocusTitle,
  disabledReason,
  agentConfigured = true,
}: {
  noteId: string
  title: string
  recordState: RecordState
  elapsedMs: number
  micError?: Error | null
  onStart: () => void
  onStop: () => void
  onDeviceChange?: (deviceId: string | undefined) => void
  onGainChange?: (gainLinear: number) => void
  recordingLevel?: number
  onMicRetry?: () => void
  onTitleSaved: (t: string) => void
  onDeleteNote: () => void
  onDuplicate: () => void
  onExport: () => void
  pinned: boolean
  onTogglePinned: () => void
  eventId?: string
  onLinkEvent?: (eventId: string) => void
  onUnlinkEvent?: () => void
  onExportPlainText?: () => void
  onExportSrt?: () => void
  onExportAss?: () => void
  onExportVtt?: () => void
  onExportWithOptions?: () => void
  serverExportFormats?: ExportFormatAction[]
  onResummarize?: () => void
  onRetranscribe?: (options: RetranscribeNoteRequest) => Promise<void>
  autoFocusTitle?: boolean
  disabledReason?: string
  agentConfigured?: boolean
}) {
  const [value, setValue] = useState(title)
  const [isDirty, setIsDirty] = useState(false)
  const [titleSaveState, setTitleSaveState] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')
  const [menuOpen, setMenuOpen] = useState(false)
  const [serverExportOpen, setServerExportOpen] = useState(false)
  const [retranscribeOpen, setRetranscribeOpen] = useState(false)
  const [retranscribeModel, setRetranscribeModel] = useState('')
  const [retranscribeLanguage, setRetranscribeLanguage] = useState('')
  const [retranscribing, setRetranscribing] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const serverExportRef = useRef<HTMLDivElement>(null)
  const toggleRef = useRef<HTMLButtonElement>(null)
  const titleInputRef = useRef<HTMLInputElement>(null)
  const titleRef = useRef(title)
  const suppressNextTitleSaveRef = useRef(false)
  const { announce } = useAnnouncer()

  // The visible menu item buttons, in DOM order, for roving focus.
  const getItems = () =>
    Array.from(
      menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [],
    )

  useEffect(() => {
    titleRef.current = title
  }, [title])

  // Keep the input in sync with external title updates unless the user has a
  // local draft in progress. Note changes always reset the draft.
  useEffect(() => {
    setValue(titleRef.current)
    setIsDirty(false)
    setTitleSaveState('idle')
  }, [noteId])

  useEffect(() => {
    if (!isDirty) setValue(title)
  }, [title, isDirty])

  // Auto-focus the title input on mount when requested (e.g. new-meeting capture mode).
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { if (autoFocusTitle) titleInputRef.current?.focus() }, [])

  // Close the menu and return focus to the toggle button.
  const closeMenu = () => {
    setServerExportOpen(false)
    setRetranscribeOpen(false)
    setMenuOpen(false)
    toggleRef.current?.focus()
  }

  useEffect(() => {
    if (!menuOpen) return
    const onDoc = (e: MouseEvent) => {
      if (!menuRef.current || !menuRef.current.contains(e.target as Node)) closeMenu()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeMenu()
    }
    // Defer the doc listener so the opening click doesn't immediately close it.
    const t = setTimeout(() => document.addEventListener('mousedown', onDoc), 0)
    document.addEventListener('keydown', onKey)
    return () => {
      clearTimeout(t)
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [menuOpen])

  // On open, move focus to the first menu item (roving-focus entry point).
  useEffect(() => {
    if (menuOpen) getItems()[0]?.focus()
  }, [menuOpen])

  useEffect(() => {
    if (!serverExportOpen) return
    serverExportRef.current?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus()
  }, [serverExportOpen])

  // Roving-focus key handling on the menu container. Arrow keys move focus
  // between visible items (wrapping); Tab/Shift+Tab cycle within the menu so
  // focus cannot escape to the page behind it. Home/End jump to first/last.
  const onMenuKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    const items = getItems()
    if (items.length === 0) return
    const current = items.indexOf(document.activeElement as HTMLButtonElement)
    const focusAt = (i: number) => {
      const n = items.length
      items[((i % n) + n) % n].focus()
    }
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        focusAt(current + 1)
        break
      case 'ArrowUp':
        e.preventDefault()
        focusAt(current - 1)
        break
      case 'Home':
        e.preventDefault()
        focusAt(0)
        break
      case 'End':
        e.preventDefault()
        focusAt(items.length - 1)
        break
      case 'Tab':
        // Keep focus trapped within the menu by cycling through the items.
        e.preventDefault()
        focusAt(e.shiftKey ? current - 1 : current + 1)
        break
    }
  }

  const canRetranscribe = Boolean(onRetranscribe)
  const submitRetranscribe = async () => {
    if (!onRetranscribe) return
    setRetranscribing(true)
    try {
      const options: RetranscribeNoteRequest = {}
      const model = retranscribeModel.trim()
      const language = retranscribeLanguage.trim()
      if (model) options.model = model
      if (language) options.language = language
      await onRetranscribe(options)
      setRetranscribeOpen(false)
      setRetranscribeModel('')
      setRetranscribeLanguage('')
      closeMenu()
    } finally {
      setRetranscribing(false)
    }
  }

  const handleTitleBlur = async () => {
    if (suppressNextTitleSaveRef.current) {
      suppressNextTitleSaveRef.current = false
      return
    }
    if (value !== title) {
      setTitleSaveState('saving')
      try {
        await muesli.updateTitle(noteId, value)
        setTitleSaveState('saved')
        setIsDirty(false)
        onTitleSaved(value)
        announce('Title saved')
      } catch {
        setTitleSaveState('error')
        announce('Could not save title')
      }
    } else {
      setIsDirty(false)
      setTitleSaveState('idle')
    }
  }

  return (
    <header className="flex items-center justify-between gap-4 border-b border-border px-6 py-4">
      <div className="min-w-0 flex-1">
        <input
          ref={titleInputRef}
          aria-label="Note title"
          value={value}
          onChange={(e) => {
            setValue(e.target.value)
            setIsDirty(true)
          }}
          onBlur={handleTitleBlur}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !e.altKey && !e.metaKey && !e.ctrlKey) {
              e.preventDefault()
              e.currentTarget.blur()
            } else if (e.key === 'Escape') {
              suppressNextTitleSaveRef.current = true
              setValue(title)
              setIsDirty(false)
              setTitleSaveState('idle')
              e.currentTarget.blur()
            }
          }}
          className="min-w-0 w-full bg-transparent font-serif text-2xl font-semibold focus:outline-none"
          placeholder="Untitled meeting"
        />
        {titleSaveState === 'error' && (
          <p className="mt-1 text-xs text-destructive" role="alert">
            Could not save title - press Enter to retry
          </p>
        )}
      </div>
      <RecordControl
        state={recordState}
        elapsedMs={elapsedMs}
        onStart={onStart}
        onStop={onStop}
        onDeviceChange={onDeviceChange}
        onGainChange={onGainChange}
        recordingLevel={recordingLevel}
        micError={micError}
        onRetry={onMicRetry}
        disabledReason={disabledReason}
      />
      {onLinkEvent && onUnlinkEvent && (
        <NoteEventLink eventId={eventId} onLink={onLinkEvent} onUnlink={onUnlinkEvent} />
      )}
      <div className="relative" ref={menuRef}>
        <button
          ref={toggleRef}
          aria-label="Note actions"
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          onClick={() => setMenuOpen((o) => !o)}
          className="inline-flex h-9 w-9 items-center justify-center rounded-[var(--radius)] text-muted-foreground hover:bg-muted"
        >
          <MoreHorizontal size={18} />
        </button>
        {menuOpen && (
          <div
            role="menu"
            aria-label="Note actions"
            tabIndex={-1}
            onKeyDown={onMenuKeyDown}
            className="absolute right-0 z-10 mt-1 w-44 rounded-[var(--radius)] border border-border bg-popover p-1 shadow-md"
          >
            {onResummarize && (
              <button
                role="menuitem"
                disabled={!agentConfigured}
                title={!agentConfigured ? 'AI features need an agent plugin configured.' : undefined}
                onClick={() => { closeMenu(); onResummarize() }}
                className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-muted disabled:cursor-not-allowed disabled:text-muted-foreground"
              >
                Re-run summary
                {!agentConfigured ? <span className="block text-xs">Agent plugin required</span> : null}
              </button>
            )}
            {canRetranscribe && (
              <button
                role="menuitem"
                onClick={() => {
                  setServerExportOpen(false)
                  setRetranscribeOpen(true)
                  setMenuOpen(false)
                }}
                className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-muted"
              >
                Enhance…
              </button>
            )}
            <button
              role="menuitem"
              onClick={() => { closeMenu(); onTogglePinned() }}
              className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-muted"
            >
              <span className="inline-flex items-center gap-2">
                <Pin size={14} />
                {pinned ? 'Unpin note' : 'Pin note'}
              </span>
            </button>
            <button
              role="menuitem"
              onClick={() => { closeMenu(); onExport() }}
              className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-muted"
            >
              Export as Markdown…
            </button>
            <button
              role="menuitem"
              onClick={() => { closeMenu(); onDuplicate() }}
              className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-muted"
            >
              Duplicate
            </button>
            {onExportPlainText && (
              <button
                role="menuitem"
                onClick={() => { closeMenu(); onExportPlainText() }}
                className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-muted"
              >
                Export as Plain Text…
              </button>
            )}
            {onExportSrt && (
              <button
                role="menuitem"
                onClick={() => { closeMenu(); onExportSrt() }}
                className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-muted"
              >
                Export as SRT…
              </button>
            )}
            {onExportAss && (
              <button
                role="menuitem"
                onClick={() => { closeMenu(); onExportAss() }}
                className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-muted"
              >
                Export as ASS…
              </button>
            )}
            {onExportVtt && (
              <button
                role="menuitem"
                onClick={() => { closeMenu(); onExportVtt() }}
                className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-muted"
              >
                Export as WebVTT (.vtt)…
              </button>
            )}
            {serverExportFormats && serverExportFormats.length > 0 && (
              <div className="relative">
                <button
                  role="menuitem"
                  aria-haspopup="menu"
                  aria-expanded={serverExportOpen}
                  onClick={() => setServerExportOpen((o) => !o)}
                  className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-muted"
                >
                  Export
                </button>
                {serverExportOpen && (
                  <div
                    ref={serverExportRef}
                    role="menu"
                    aria-label="Export formats"
                    className="absolute left-full top-0 z-20 ml-1 w-48 rounded-[var(--radius)] border border-border bg-popover p-1 shadow-md"
                  >
                    {onExportWithOptions && (
                      <button
                        role="menuitem"
                        onClick={() => { closeMenu(); onExportWithOptions() }}
                        className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-muted"
                      >
                        Export with options…
                      </button>
                    )}
                    {serverExportFormats.map((format) => (
                      <button
                        key={format.id}
                        role="menuitem"
                        onClick={() => { closeMenu(); format.onSelect() }}
                        className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-muted"
                      >
                        {format.label}…
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}
            <button
              role="menuitem"
              onClick={() => { closeMenu(); onDeleteNote() }}
              className="block w-full rounded px-2 py-1 text-left text-sm text-destructive hover:bg-muted"
            >
              Move to Trash
            </button>
          </div>
        )}
      </div>
      {retranscribeOpen && (
        <Dialog open onOpenChange={(open) => { if (!open) setRetranscribeOpen(false) }} title="Re-transcribe note">
          <form
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault()
              void submitRetranscribe()
            }}
          >
            <p className="text-sm text-muted-foreground">
              Leave the overrides blank to use the note&apos;s existing settings.
            </p>
            <label className="block space-y-1 text-sm">
              <span className="font-medium">Model override</span>
              <input
                value={retranscribeModel}
                onChange={(e) => setRetranscribeModel(e.target.value)}
                placeholder="Optional"
                className="w-full rounded-[var(--radius)] border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </label>
            <label className="block space-y-1 text-sm">
              <span className="font-medium">Language override</span>
              <input
                value={retranscribeLanguage}
                onChange={(e) => setRetranscribeLanguage(e.target.value)}
                placeholder="Optional"
                className="w-full rounded-[var(--radius)] border border-border bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
              />
            </label>
            <div className="flex justify-end gap-2 pt-2">
              <Button type="button" variant="secondary" size="sm" onClick={() => setRetranscribeOpen(false)} disabled={retranscribing}>
                Cancel
              </Button>
              <Button type="submit" size="sm" disabled={retranscribing}>
                {retranscribing ? 'Enhancing…' : 'Re-transcribe'}
              </Button>
            </div>
          </form>
        </Dialog>
      )}
    </header>
  )
}
