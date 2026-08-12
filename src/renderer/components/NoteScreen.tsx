import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams, useOutletContext } from 'react-router-dom'
import { ChevronRight } from 'lucide-react'
import { muesli } from '@/api'
import { RecordingSession } from '../../main/recorder'
import { ElectronCapture, MicPermissionDeniedError } from '../capture/electronCapture'
import { startSystemAudioStream, type SystemAudioStreamHandle } from '../capture/systemAudioStream'
import { pollNote } from '@/lib/pollNote'
import { isProcessing } from '@/lib/status'
import { useToast } from '@/components/ui/Toast'
import { useActivity } from '@/lib/activityStore'
import { NoteHeader } from './NoteHeader'
import { ProcessingBanner } from './ProcessingBanner'
import { TagBar } from './TagBar'
import { FolderBar } from './FolderBar'
import { NoteActionItemsPanel } from './NoteActionItemsPanel'
import { LiveTranscriptPanel } from './LiveTranscriptPanel'
import { tagIndex } from '@/lib/tagIndex'
import { loadAudioPrefs, saveAudioPrefs } from '@/lib/audioPrefs'
import { useAnnouncer } from '@/hooks/useAnnouncer'
import { useAgentCapability } from '@/lib/agentCapability'
import { AI_UNAVAILABLE_MESSAGE } from './AgentUnavailableNotice'
// Lazy so TipTap (the renderer-bundle bulk) is only fetched when an editor mounts.
const NoteEditor = lazy(() => import('./NoteEditor').then((m) => ({ default: m.NoteEditor })))
import { NoteView } from './NoteView'
import { fullNoteToMarkdown, fullNoteToPlainText } from '@/lib/noteMarkdown'
import { transcriptToPlainText } from '@/lib/transcriptText'
import { renderFilenameTemplate } from '@/lib/filenameTemplate'
import { buildSubtitleCues } from '@/lib/subtitleCues'
import { cuesToSrt, cuesToAss, cuesToVtt } from '@/lib/subtitleFormats'
import { writeClipboardText } from '@/lib/clipboard'
import { Dialog } from '@/components/ui/Dialog'
import { Button } from '@/components/ui/Button'
import type { CreateShareRequest, FullNote, Folder, Note, NoteLink, NoteLinksResponse, RelatedNote, Share, Template } from '../../shared/types'
import { isTerminal } from '../../shared/types'
import { Skeleton } from './ui/Skeleton'
import type { RecordState } from './RecordControl'
import { DuplicateAudioDialog } from './DuplicateAudioDialog'
import { ExportOptionsDialog, type ExportOptionsValue } from './ExportOptionsDialog'
import { recordUnavailableReason } from '@/lib/recordAvailability'

interface Ctx { notes: Note[]; allNotes: Note[]; folders: Folder[]; refresh: () => void }

function linkedNoteId(link: NoteLink, currentNoteId: string): string {
  if (link.from_note_id === currentNoteId) return link.to_note_id
  if (link.to_note_id === currentNoteId) return link.from_note_id
  return link.to_note_id
}

function noteTitleForId(allNotes: Note[], noteId: string): string {
  return allNotes.find((note) => note.id === noteId)?.title || noteId
}

function isActiveShare(share: Share): boolean {
  if (share.revoked_at) return false
  if (!share.expires_at) return true
  const expiresAt = new Date(share.expires_at).getTime()
  return Number.isNaN(expiresAt) ? true : expiresAt > Date.now()
}

function formatShareDate(iso?: string | null): string {
  if (!iso) return 'No expiry'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleString()
}

function toRfc3339(value: string): string | undefined {
  if (!value.trim()) return undefined
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return undefined
  return date.toISOString()
}

type LinkRefreshHandler = () => void

interface NoteLinksPanelProps {
  noteId: string
  allNotes: Note[]
  onOpenNote: (id: string) => void
  refreshToken: number
}

function NoteSharesPanel({ noteId }: { noteId: string }) {
  const [shares, setShares] = useState<Share[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [expiresAt, setExpiresAt] = useState('')
  const [creating, setCreating] = useState(false)
  const [statusMessage, setStatusMessage] = useState<string | null>(null)
  const { announce, announceAssertive } = useAnnouncer()

  const loadShares = useCallback(async () => {
    if (!noteId) return
    setShares(null)
    setError(null)
    try {
      const res = await muesli.listNoteShares(noteId)
      setShares(res ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not load shares')
    }
  }, [noteId])

  useEffect(() => {
    void loadShares()
  }, [loadShares])

  useEffect(() => {
    setExpiresAt('')
    setStatusMessage(null)
  }, [noteId])

  if (!noteId) return null

  const activeShares = (shares ?? [])
    .filter(isActiveShare)
    .slice()
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())

  const createShare = async () => {
    try {
      setCreating(true)
      setError(null)
      const expiresAtValue = toRfc3339(expiresAt)
      const request: CreateShareRequest | undefined = expiresAtValue ? { expires_at: expiresAtValue } : undefined
      const created = await muesli.createShare(noteId, request)
      await writeClipboardText(created.url)
      setStatusMessage(`Share link copied to clipboard: ${created.url}`)
      setExpiresAt('')
      announce('Share link copied to clipboard')
      await loadShares()
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Could not create share link'
      setError(message)
      announceAssertive(message)
    } finally {
      setCreating(false)
    }
  }

  const revokeShare = async (token: string) => {
    const previous = shares
    setShares((current) => current?.filter((share) => share.token !== token) ?? current)
    try {
      await muesli.revokeShare(token)
      announce('Share revoked')
    } catch (err) {
      setShares(previous)
      setError(err instanceof Error ? err.message : 'Could not revoke share')
    }
  }

  return (
    <section className="border-b border-border px-6 py-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">Sharing</h2>
      </div>

      <p className="mt-3 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-foreground">
        Public link: anyone who has it can read this note.
      </p>

      {error ? (
        <div role="alert" className="mt-3 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          Could not update shares.
          <div className="mt-1 text-xs text-destructive/80">{error}</div>
        </div>
      ) : null}

      <div className="mt-4 rounded-lg border border-border bg-background/70 p-3">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end">
          <label className="flex-1 text-sm font-medium">
            Expires at
            <input
              type="datetime-local"
              value={expiresAt}
              onChange={(e) => setExpiresAt(e.target.value)}
              className="mt-2 w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-2 focus:ring-ring/20"
            />
          </label>
          <Button type="button" size="sm" onClick={() => { void createShare() }} disabled={creating}>
            {creating ? 'Creating...' : 'Create share link'}
          </Button>
        </div>
        <p className="mt-2 text-xs text-muted-foreground">
          Leave blank for no expiry. The link is public to anyone with the URL.
        </p>
      </div>

      {statusMessage ? (
        <div role="status" className="mt-3 rounded-lg border border-border bg-background/70 px-3 py-2 text-sm text-foreground">
          {statusMessage}
        </div>
      ) : null}

      {shares === null && error === null ? (
        <div role="status" className="mt-3 text-sm text-muted-foreground">
          Loading shares...
        </div>
      ) : activeShares.length === 0 ? (
        <p className="mt-3 text-sm text-muted-foreground">No active shares</p>
      ) : (
        <ul className="mt-4 space-y-2">
          {activeShares.map((share) => (
            <li key={share.id} className="flex items-start justify-between gap-3 rounded-lg border border-border bg-background/70 px-3 py-2">
              <div className="min-w-0">
                <p className="text-sm font-medium">Public share</p>
                <p className="mt-1 text-xs text-muted-foreground">Created: {formatShareDate(share.created_at)}</p>
                <p className="text-xs text-muted-foreground">Expires: {formatShareDate(share.expires_at)}</p>
              </div>
              <Button type="button" variant="secondary" size="sm" onClick={() => { void revokeShare(share.token) }}>
                Revoke
              </Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function NoteLinksPanel({ noteId, allNotes, onOpenNote, refreshToken }: NoteLinksPanelProps) {
  const [links, setLinks] = useState<NoteLinksResponse | null>(null)
  const [linksError, setLinksError] = useState<string | null>(null)
  const [addLinkError, setAddLinkError] = useState<string | null>(null)
  const [notes, setNotes] = useState<Note[] | null>(null)
  const [notesError, setNotesError] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [pickerDismissed, setPickerDismissed] = useState(false)
  const [activeIndex, setActiveIndex] = useState(0)
  const inputRef = useRef<HTMLInputElement | null>(null)
  const optionRefs = useRef<Array<HTMLButtonElement | null>>([])

  const loadLinks = useCallback(async () => {
    if (!noteId) return
    setLinks(null)
    setLinksError(null)
    try {
      const res = await muesli.listNoteLinks(noteId)
      setLinks({
        outgoing: res.outgoing ?? [],
        backlinks: res.backlinks ?? [],
      })
    } catch (err) {
      setLinksError(err instanceof Error ? err.message : 'Could not load links')
    }
  }, [noteId, refreshToken])

  useEffect(() => {
    void loadLinks()
  }, [loadLinks])

  useEffect(() => {
    let cancelled = false
    setNotes(null)
    setNotesError(null)

    const listNotes = muesli.listNotes
    if (typeof listNotes !== 'function') {
      setNotes([])
      return () => {
        cancelled = true
      }
    }

    void listNotes()
      .then((res) => {
        if (cancelled) return
        setNotes(res ?? [])
      })
      .catch((err) => {
        if (cancelled) return
        setNotes([])
        setNotesError(err instanceof Error ? err.message : 'Could not load notes')
      })

    return () => {
      cancelled = true
    }
  }, [])

  if (!noteId) return null

  const outgoing = links?.outgoing ?? []
  const backlinks = links?.backlinks ?? []
  const hasAnyLinks = outgoing.length > 0 || backlinks.length > 0
  const loading = links === null && linksError === null
  const noteList = notes ?? []
  const filteredNotes = query.trim()
    ? noteList.filter((note) => note.id !== noteId && note.title.toLowerCase().includes(query.trim().toLowerCase()))
    : []
  const showPicker = query.trim().length > 0 && !pickerDismissed
  const activeCandidate = filteredNotes[activeIndex] ?? filteredNotes[0] ?? null

  const focusCandidate = (index: number) => {
    if (filteredNotes.length === 0) return
    const nextIndex = Math.max(0, Math.min(index, filteredNotes.length - 1))
    setActiveIndex(nextIndex)
    optionRefs.current[nextIndex]?.focus()
  }

  const selectCandidate = async (candidate: Note) => {
    try {
      setAddLinkError(null)
      await muesli.addNoteLink(noteId, candidate.id)
      setQuery('')
      setPickerDismissed(false)
      setActiveIndex(0)
      await loadLinks()
      inputRef.current?.focus()
    } catch (err) {
      setAddLinkError(err instanceof Error ? err.message : 'Could not add link')
    }
  }

  return (
    <section className="border-b border-border px-6 py-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">Links</h2>
      </div>

      <div className="mt-4 rounded-lg border border-border bg-background/40 p-3">
        <label htmlFor={`add-link-${noteId}`} className="text-sm font-medium">
          Add link
        </label>
        <input
          ref={inputRef}
          id={`add-link-${noteId}`}
          type="text"
          autoComplete="off"
          className="mt-2 w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-ring focus:ring-2 focus:ring-ring/20"
          placeholder="Type a note title"
          value={query}
          aria-describedby={`add-link-help-${noteId}`}
          onChange={(e) => {
            setQuery(e.target.value)
            setPickerDismissed(false)
            setAddLinkError(null)
            setActiveIndex(0)
          }}
          onFocus={() => setPickerDismissed(false)}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              if (showPicker) {
                e.preventDefault()
                setPickerDismissed(true)
              }
              return
            }

            if (filteredNotes.length === 0) return

            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setPickerDismissed(false)
              focusCandidate(0)
              return
            }

            if (e.key === 'ArrowUp') {
              e.preventDefault()
              setPickerDismissed(false)
              focusCandidate(filteredNotes.length - 1)
              return
            }

            if (e.key === 'Enter' && showPicker) {
              e.preventDefault()
              void selectCandidate(activeCandidate ?? filteredNotes[0])
            }
          }}
        />
        <p id={`add-link-help-${noteId}`} className="mt-1 text-xs text-muted-foreground">
          Type to filter other notes. Use Tab or Arrow keys to move through matches.
        </p>
        {notesError ? (
          <div role="alert" className="mt-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
            Could not load notes.
            <div className="mt-1 text-destructive/80">{notesError}</div>
          </div>
        ) : showPicker ? (
          filteredNotes.length > 0 ? (
            <ul aria-label="Matching notes" className="mt-3 space-y-2">
              {filteredNotes.map((note, index) => (
                <li key={note.id}>
                  <button
                    ref={(el) => {
                      optionRefs.current[index] = el
                    }}
                    type="button"
                    aria-label={note.title}
                    className={`flex w-full items-center justify-between gap-3 rounded-lg border px-3 py-2 text-left text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                      index === activeIndex
                        ? 'border-ring bg-accent/40 text-foreground'
                        : 'border-border bg-background/70 text-foreground hover:bg-accent/30'
                    }`}
                    onFocus={() => setActiveIndex(index)}
                    onKeyDown={(e) => {
                      if (e.key === 'ArrowDown') {
                        e.preventDefault()
                        focusCandidate(index + 1)
                      } else if (e.key === 'ArrowUp') {
                        e.preventDefault()
                        if (index === 0) {
                          inputRef.current?.focus()
                        } else {
                          focusCandidate(index - 1)
                        }
                      } else if (e.key === 'Escape') {
                        e.preventDefault()
                        setPickerDismissed(true)
                        inputRef.current?.focus()
                      }
                    }}
                    onClick={() => {
                      void selectCandidate(note)
                    }}
                  >
                    <span className="truncate">{note.title}</span>
                    <span className="shrink-0 text-xs text-muted-foreground">{note.id}</span>
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="mt-3 text-sm text-muted-foreground">No matching notes</p>
          )
        ) : query.trim() ? (
          <p className="mt-3 text-sm text-muted-foreground">Press Escape to hide matches</p>
        ) : (
          <p className="mt-3 text-sm text-muted-foreground">Start typing a note title to add a link.</p>
        )}
        {addLinkError ? (
          <div role="alert" className="mt-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
            Could not add link.
            <div className="mt-1 text-destructive/80">{addLinkError}</div>
          </div>
        ) : null}
        {notes === null && !notesError ? <p className="mt-2 text-xs text-muted-foreground">Loading note list...</p> : null}
      </div>

      {linksError ? (
        <div role="alert" className="mt-3 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          Could not load links.
          <div className="mt-1 text-xs text-destructive/80">{linksError}</div>
        </div>
      ) : loading ? (
        <div role="status" className="mt-3 space-y-2">
          <p className="text-sm text-muted-foreground">Loading links...</p>
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : !hasAnyLinks ? (
        <p className="mt-3 text-sm text-muted-foreground">No links yet</p>
      ) : (
        <div className="mt-4 space-y-5">
          <div>
            <h3 className="text-sm font-medium">Outgoing</h3>
            {outgoing.length > 0 ? (
              <ul className="mt-2 space-y-2">
                {outgoing.map((link) => {
                  const otherNoteId = linkedNoteId(link, noteId)
                  return (
                    <li key={link.id}>
                      <button
                        type="button"
                        className="flex w-full items-center justify-between gap-3 rounded-lg border border-border bg-background/70 px-3 py-2 text-left text-sm text-foreground transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        onClick={() => onOpenNote(otherNoteId)}
                      >
                        <span className="truncate">{noteTitleForId(allNotes, otherNoteId)}</span>
                      </button>
                    </li>
                  )
                })}
              </ul>
            ) : (
              <p className="mt-2 text-sm text-muted-foreground">No outgoing links</p>
            )}
          </div>

          <div>
            <h3 className="text-sm font-medium">Referenced by</h3>
            {backlinks.length > 0 ? (
              <ul className="mt-2 space-y-2">
                {backlinks.map((link) => {
                  const otherNoteId = linkedNoteId(link, noteId)
                  return (
                    <li key={link.id}>
                      <button
                        type="button"
                        className="flex w-full items-center justify-between gap-3 rounded-lg border border-border bg-background/70 px-3 py-2 text-left text-sm text-foreground transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        onClick={() => onOpenNote(otherNoteId)}
                      >
                        <span className="truncate">{noteTitleForId(allNotes, otherNoteId)}</span>
                      </button>
                    </li>
                  )
                })}
              </ul>
            ) : (
              <p className="mt-2 text-sm text-muted-foreground">No references yet</p>
            )}
          </div>
        </div>
      )}
    </section>
  )
}

interface RelatedNotesPanelProps {
  noteId: string
  allNotes: Note[]
  onOpenNote: (id: string) => void
  refreshToken: number
  onLinked?: LinkRefreshHandler
}

export function RelatedNotesPanel({ noteId, allNotes, onOpenNote, refreshToken, onLinked }: RelatedNotesPanelProps) {
  const [related, setRelated] = useState<RelatedNote[] | null>(null)
  const [relatedError, setRelatedError] = useState<string | null>(null)
  const [relatedLinkError, setRelatedLinkError] = useState<string | null>(null)

  const loadRelated = useCallback(async () => {
    if (!noteId) return
    setRelated(null)
    setRelatedError(null)
    setRelatedLinkError(null)
    try {
      const res = await muesli.listRelatedNotes(noteId)
      setRelated(res ?? [])
    } catch (err) {
      setRelatedError(err instanceof Error ? err.message : 'Could not load related notes')
    }
  }, [noteId, refreshToken])

  useEffect(() => {
    void loadRelated()
  }, [loadRelated])

  if (!noteId) return null

  const loading = related === null && relatedError === null

  const linkSuggestion = async (candidate: RelatedNote) => {
    try {
      setRelatedLinkError(null)
      await muesli.addNoteLink(noteId, candidate.note_id)
      setRelated((current) => current?.filter((item) => item.note_id !== candidate.note_id) ?? current)
      onLinked?.()
    } catch (err) {
      setRelatedLinkError(err instanceof Error ? err.message : 'Could not add link')
    }
  }

  return (
    <section className="border-b border-border px-6 py-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">Related</h2>
      </div>

      {relatedLinkError ? (
        <div role="alert" className="mt-3 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          Could not add link.
          <div className="mt-1 text-xs text-destructive/80">{relatedLinkError}</div>
        </div>
      ) : null}

      {relatedError ? (
        <div role="alert" className="mt-3 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          Could not load related notes.
          <div className="mt-1 text-xs text-destructive/80">{relatedError}</div>
        </div>
      ) : loading ? (
        <div role="status" className="mt-3 space-y-2">
          <p className="text-sm text-muted-foreground">Loading related notes...</p>
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : (related ?? []).length === 0 ? (
        <p className="mt-3 text-sm text-muted-foreground">No related suggestions</p>
      ) : (
        <ul className="mt-4 space-y-2">
          {(related ?? []).map((candidate) => (
            <li key={candidate.note_id}>
              <div className="flex items-center justify-between gap-3 rounded-lg border border-border bg-background/70 px-3 py-2">
                <button
                  type="button"
                  className="min-w-0 flex-1 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  onClick={() => onOpenNote(candidate.note_id)}
                >
                  <span className="block truncate text-sm font-medium">{noteTitleForId(allNotes, candidate.note_id)}</span>
                  <span className="text-xs text-muted-foreground">Score {candidate.score.toFixed(2)}</span>
                </button>
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={() => {
                    void linkSuggestion(candidate)
                  }}
                >
                  Link
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      {relatedLinkError ? (
        <div role="alert" className="mt-3 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          Could not add link.
          <div className="mt-1 text-xs text-destructive/80">{relatedLinkError}</div>
        </div>
      ) : null}
    </section>
  )
}

export function NoteScreen() {
  const { id = '' } = useParams()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { allNotes, folders, refresh } = useOutletContext<Ctx>()
  const { notify } = useToast()
  const { addUpload, updateUpload, addProcessing, updateProcessing } = useActivity()
  const agentConfigured = useAgentCapability()

  const [full, setFull] = useState<FullNote | null>(null)
  const [templates, setTemplates] = useState<Template[]>([])
  const [regeneratingTemplateId, setRegeneratingTemplateId] = useState<string | null>(null)
  const [linkRefreshToken, setLinkRefreshToken] = useState(0)
  const [loadError, setLoadError] = useState(false)
  const [retryCount, setRetryCount] = useState(0)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [exportOptionsOpen, setExportOptionsOpen] = useState(false)
  const [detailsOpen, setDetailsOpen] = useState(false)
  const [recordState, setRecordState] = useState<RecordState>('idle')
  const [elapsedMs, setElapsedMs] = useState(0)
  const [micError, setMicError] = useState<Error | null>(null)
  const [microphoneLevel, setMicrophoneLevel] = useState(0)
  const [transcriptCopied, setTranscriptCopied] = useState(false)
  const [pendingUpload, setPendingUpload] = useState<{
    audio: ArrayBuffer
    mimeType: string
    existingNoteId: string
    existingNoteTitle: string
  } | null>(null)
  const sessionRef = useRef<RecordingSession | null>(null)
  const noteIdRef = useRef(id)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const pollAbortRef = useRef<AbortController | null>(null)
  const tagAbortRef = useRef<AbortController | null>(null)
  const autostartConsumedRef = useRef(false)
  const transcriptCopyTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const mountedRef = useRef(true)
  // Stores the onUploadProgress unsubscribe function; cleaned up on unmount.
  const uploadProgressUnsubRef = useRef<(() => void) | null>(null)
  const systemAudioHandleRef = useRef<SystemAudioStreamHandle | null>(null)

  // Audio prefs — loaded on mount and kept in sync with RecordControl callbacks.
  const [selectedDeviceId, setSelectedDeviceId] = useState<string | undefined>(() => loadAudioPrefs().deviceId)
  const [gainLinear, setGainLinear] = useState<number>(() => loadAudioPrefs().gain)

  useEffect(() => {
    const abort = new AbortController()
    muesli.getFull(id).then((f) => {
      setFull(f)
      setLoadError(false)
      if (isProcessing(f.note.status)) {
        setRecordState('processing')
        addProcessing(id, f.note.title, f.note.status)
        void pollNote(id, muesli.getFull, {
          signal: abort.signal,
          onUpdate: (updated) => {
            setFull(updated)
            updateProcessing(id, updated.note.status, isTerminal(updated.note.status))
          },
        })
          .then((done) => {
            setFull(done)
            setRecordState('idle')
            refresh()
          })
          .catch(() => {})
      }
    }).catch((err) => {
      notify(err instanceof Error ? err.message : 'Could not load note', 'error')
      setLoadError(true)
    })
    return () => abort.abort()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, refresh, retryCount])

  // Fetches every template visible to the owner (built-ins + their own) so
  // NoteView's picker can list templates that don't have a summary for this
  // note yet. Resolves asynchronously — NoteView's selection reconciliation
  // handles the resulting entries reorder/growth (see TPL01).
  useEffect(() => {
    let cancelled = false
    const list = muesli?.listTemplates
    if (typeof list !== 'function') return
    list().then((t) => { if (!cancelled) setTemplates(t ?? []) }).catch(() => {})
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      if (timerRef.current) clearInterval(timerRef.current)
      pollAbortRef.current?.abort()
      tagAbortRef.current?.abort()
      void muesli.stopNoteStream?.(noteIdRef.current)?.catch(() => {})
      systemAudioHandleRef.current?.stop()
      systemAudioHandleRef.current = null
      uploadProgressUnsubRef.current?.()
      uploadProgressUnsubRef.current = null
    }
  }, [])

  useEffect(() => {
    noteIdRef.current = id
  }, [id])

  useEffect(() => {
    autostartConsumedRef.current = false
  }, [id])

  useEffect(() => {
    setTranscriptCopied(false)
    if (transcriptCopyTimerRef.current) {
      clearTimeout(transcriptCopyTimerRef.current)
      transcriptCopyTimerRef.current = null
    }
  }, [id])

  useEffect(() => {
    return () => {
      tagAbortRef.current?.abort()
    }
  }, [id])

  useEffect(() => {
    return () => {
      if (transcriptCopyTimerRef.current) clearTimeout(transcriptCopyTimerRef.current)
    }
  }, [])

  const retryPipeline = useCallback(async () => {
    if (!id) return
    try {
      await muesli.retryNote(id)
      refresh()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not retry the pipeline', 'error')
    }
  }, [id, refresh, notify])

  const stopSystemAudio = useCallback(async () => {
    const handle = systemAudioHandleRef.current
    systemAudioHandleRef.current = null
    if (handle) handle.stop()
    else await Promise.resolve(muesli.systemAudioStop?.()).catch(() => {})
  }, [])

  // Bumps this note's still-pending job(s) to the front of the queue so they
  // dequeue before other queued notes. Does not touch anything already running.
  const processNext = useCallback(async () => {
    if (!id) return
    try {
      await muesli.processNextNote(id)
      refresh()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not prioritize this note', 'error')
    }
  }, [id, refresh, notify])

  const togglePinned = useCallback(async () => {
    if (!id || !full) return
    try {
      if (full.note.pinned) await muesli.unpinNote(id)
      else await muesli.pinNote(id)
      const updated = await muesli.getFull(id)
      setFull(updated)
      refresh()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not update note pin state', 'error')
    }
  }, [full, id, notify, refresh])

  const linkEvent = useCallback(async (eventId: string) => {
    if (!id) return
    try {
      await muesli.linkNoteEvent(id, eventId)
      const updated = await muesli.getFull(id)
      setFull(updated)
      refresh()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not link calendar event', 'error')
    }
  }, [id, notify, refresh])

  const unlinkEvent = useCallback(async () => {
    if (!id) return
    try {
      await muesli.unlinkNoteEvent(id)
      const updated = await muesli.getFull(id)
      setFull(updated)
      refresh()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not unlink calendar event', 'error')
    }
  }, [id, notify, refresh])

  const duplicateNote = useCallback(async () => {
    if (!id) return
    try {
      const copy = await muesli.duplicateNote(id)
      refresh()
      navigate(`/notes/${copy.id}`)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not duplicate note', 'error')
    }
  }, [id, navigate, notify, refresh])

  const refreshAfterTagMutation = useCallback(async (mutate: () => Promise<unknown>, errorMessage: string): Promise<void> => {
    tagAbortRef.current?.abort()
    const controller = new AbortController()
    tagAbortRef.current = controller

    try {
      await mutate()
      if (controller.signal.aborted || !mountedRef.current) return

      const updated = await muesli.getFull(id)
      if (controller.signal.aborted || !mountedRef.current) return

      setFull(updated)
      refresh()
    } catch (err) {
      if (!mountedRef.current || controller.signal.aborted) return
      notify(err instanceof Error ? err.message : errorMessage, 'error')
    }
  }, [id, notify, refresh])

  async function reRunSummary() {
    try {
      await muesli.resummarize(id)
      const updated = await muesli.getFull(id)
      setFull(updated)
      refresh()
      pollAbortRef.current?.abort()
      pollAbortRef.current = new AbortController()
      void pollNote(id, muesli.getFull, {
        signal: pollAbortRef.current.signal,
        onUpdate: setFull,
      })
        .then((done) => {
          if (pollAbortRef.current?.signal.aborted) return
          setFull(done)
          setRecordState('idle')
          refresh()
        })
        .catch(() => {})
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Could not re-run the summary'
      notify(message === 'no default agent configured' ? AI_UNAVAILABLE_MESSAGE : message, 'error')
    }
  }

  const beginRetranscribePoll = useCallback(() => {
    pollAbortRef.current?.abort()
    pollAbortRef.current = new AbortController()
    void pollNote(id, muesli.getFull, {
      signal: pollAbortRef.current.signal,
      onUpdate: (updated) => {
        setFull(updated)
        updateProcessing(id, updated.note.status, isTerminal(updated.note.status))
      },
    })
      .then((done) => {
        if (pollAbortRef.current?.signal.aborted) return
        setFull(done)
        setRecordState('idle')
        refresh()
      })
      .catch(() => {})
  }, [id, refresh, updateProcessing])

  const retranscribeNote = useCallback(async (options: { model?: string; language?: string }) => {
    if (!full) return
    try {
      await muesli.retranscribeNote(id, options)
      const startedAt = new Date().toISOString()
      setFull((current) => current ? {
        ...current,
        note: {
          ...current.note,
          status: 'transcribing',
          updated_at: startedAt,
        },
      } : current)
      addProcessing(id, full.note.title, 'transcribing')
      refresh()
      beginRetranscribePoll()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not re-transcribe the note', 'error')
    }
  }, [addProcessing, beginRetranscribePoll, full, id, notify, refresh])

  // Regenerates a single template's summary without touching the note's other
  // summaries or re-transcribing. Modeled on reRunSummary(); tracked per-template
  // via regeneratingTemplateId so the control disables/spinners only for the
  // template actually in flight, and can't be double-clicked into duplicate jobs.
  async function regenerateTemplate(templateId: string) {
    setRegeneratingTemplateId(templateId)
    const clear = () => setRegeneratingTemplateId((cur) => (cur === templateId ? null : cur))
    try {
      await muesli.regenerateSummary(id, templateId)
      const updated = await muesli.getFull(id)
      setFull(updated)
      refresh()
      pollAbortRef.current?.abort()
      pollAbortRef.current = new AbortController()
      void pollNote(id, muesli.getFull, {
        signal: pollAbortRef.current.signal,
        onUpdate: setFull,
      })
        .then((done) => {
          if (pollAbortRef.current?.signal.aborted) return
          setFull(done)
          setRecordState('idle')
          refresh()
        })
        .catch(() => {})
        .finally(clear)
    } catch (err) {
      clear()
      notify(err instanceof Error ? err.message : 'Could not regenerate the summary', 'error')
    }
  }

  const start = useCallback(async () => {
    setMicError(null)
    try {
      const micStatus = await muesli.micRequest?.()
      if (micStatus === 'denied' || micStatus === 'restricted') {
        throw new MicPermissionDeniedError()
      }
      void muesli.startNoteStream?.(id)?.catch(() => {})
      const session = new RecordingSession(
        new ElectronCapture({
          deviceId: selectedDeviceId,
          gainLinear,
          onLevel: setMicrophoneLevel,
          onPcmFrame: (frame) => {
            void muesli.sendNoteStreamAudio?.(id, frame)?.catch(() => {})
          },
          // System audio: inject the main-process tap PCM as a MediaStream
          // (mic-left / system-right). Returns null when unsupported -> mic-only.
          getSystemAudioStream: async () => {
            systemAudioHandleRef.current = await startSystemAudioStream()
            return systemAudioHandleRef.current?.stream ?? null
          },
        }),
        { onWarning: (w) => notify(w, 'info') },
      )
      sessionRef.current = session
      await session.start()
      setRecordState('recording')
      setElapsedMs(0)
      const startedAt = Date.now()
      timerRef.current = setInterval(() => setElapsedMs(Date.now() - startedAt), 500)
    } catch (err) {
      void muesli.stopNoteStream?.(id)?.catch(() => {})
      await stopSystemAudio()
      if (timerRef.current) clearInterval(timerRef.current)
      setRecordState('idle')
      if (
        err instanceof Error &&
        ((err as { code?: string }).code === 'mic-permission-denied' ||
          err.name === 'MicPermissionDeniedError' ||
          err.name === 'NotAllowedError')
      ) {
        setMicError(err)
      } else if (
        err instanceof Error &&
        ((err as { code?: string }).code === 'mic-device-invalid' ||
          err.name === 'MicDeviceInvalidError')
      ) {
        setMicError(err)
        // Clear the saved device prefs so retry uses the default mic.
        saveAudioPrefs({ deviceId: undefined, gain: gainLinear })
        setSelectedDeviceId(undefined)
      } else {
        notify(err instanceof Error ? err.message : 'Could not start recording', 'error')
      }
    }
  }, [gainLinear, id, notify, selectedDeviceId, stopSystemAudio])

  async function doUpload(audio: ArrayBuffer, mimeType: string) {
    setRecordState('processing')
    pollAbortRef.current = new AbortController()

    addUpload(id, full?.note.title ?? '')
    const unsub = muesli.onUploadProgress((p) => {
      if (p.noteId !== id) return
      updateUpload(id, p.phase, p.phase === 'done' || p.phase === 'error')
    })
    uploadProgressUnsubRef.current = unsub

    try {
      await muesli.uploadAudio({ noteId: id, audio, audioMimeType: mimeType })
      refresh()

      addProcessing(id, full?.note.title ?? '', 'uploaded')
      const done = await pollNote(id, muesli.getFull, {
        signal: pollAbortRef.current.signal,
        onUpdate: (f) => {
          setFull(f)
          updateProcessing(id, f.note.status, isTerminal(f.note.status))
        },
      })
      uploadProgressUnsubRef.current?.()
      uploadProgressUnsubRef.current = null

      if (pollAbortRef.current?.signal.aborted) return
      setFull(done)
      setRecordState('idle')
      refresh()
      navigate(`/notes/${id}`, { replace: true })
    } catch (err) {
      uploadProgressUnsubRef.current?.()
      uploadProgressUnsubRef.current = null

      if (err instanceof Error && err.message === 'aborted') return
      void muesli.stopNoteStream?.(id)?.catch(() => {})
      setRecordState('idle')
      notify(err instanceof Error ? err.message : 'Upload failed', 'error')
    }
  }

  async function stop() {
    const session = sessionRef.current
    if (!session) return
    if (timerRef.current) clearInterval(timerRef.current)
    setMicrophoneLevel(0)
    try {
      const result = await session.stop()
      void muesli.stopNoteStream?.(id)?.catch(() => {})
      setRecordState('processing')
      const audio = result.bytes.slice().buffer as ArrayBuffer

      try {
        const check = await muesli.checkAudioDedup(audio)
        if (check.existingNoteId) {
          setPendingUpload({
            audio,
            mimeType: result.mimeType,
            existingNoteId: check.existingNoteId,
            existingNoteTitle: check.existingNoteTitle ?? '',
          })
          setRecordState('idle')
          return
        }
      } catch {
        // network error — proceed with upload
      }

      await doUpload(audio, result.mimeType)
    }
    finally {
      await stopSystemAudio()
    }
  }

  const exportServerNote = useCallback(async (format: string, options?: ExportOptionsValue) => {
    try {
      const result = await muesli.exportNote(id, format, options)
      if (!result.success && result.error !== 'cancelled') {
        notify(result.error, 'error')
      }
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not export note', 'error')
    }
  }, [id, notify])

  const serverExportFormats = [
    { id: 'md', label: 'Markdown', onSelect: () => { void exportServerNote('md') } },
    { id: 'txt', label: 'Plain Text', onSelect: () => { void exportServerNote('txt') } },
    { id: 'docx', label: 'Word', onSelect: () => { void exportServerNote('docx') } },
  ]

  const recordDisabledReason: string | undefined = loadError
    ? 'Server unreachable'
    : full
      ? recordUnavailableReason(full.note.status)
      : undefined

  const capture = params.get('capture') === '1'
  const autostart = params.get('autostart') === '1'
  const initialSegmentId = params.get('segment') || undefined
  // Set by a global-chat citation chip's navigation (ChatScreen's
  // onCiteClick) -- already a resolved transcript-segment array index, so no
  // id lookup is needed (see NoteView's `initialSegmentIndex` prop).
  const rawSegmentIndex = params.get('segment_index')
  const parsedSegmentIndex = rawSegmentIndex != null ? Number(rawSegmentIndex) : NaN
  const initialSegmentIndex = Number.isFinite(parsedSegmentIndex) ? parsedSegmentIndex : undefined
  const transcriptSegments = full?.transcript?.segments ?? []

  const handleCopyTranscript = useCallback(async () => {
    if (!transcriptSegments.length) return
    try {
      await writeClipboardText(transcriptToPlainText(transcriptSegments))
      setTranscriptCopied(true)
      if (transcriptCopyTimerRef.current) clearTimeout(transcriptCopyTimerRef.current)
      transcriptCopyTimerRef.current = setTimeout(() => {
        setTranscriptCopied(false)
        transcriptCopyTimerRef.current = null
      }, 2000)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not copy transcript', 'error')
    }
  }, [notify, transcriptSegments])

  useEffect(() => {
    if (!autostart || autostartConsumedRef.current) return
    if (recordState === 'recording' || micError) return
    autostartConsumedRef.current = true
    const nextParams = new URLSearchParams(params)
    nextParams.delete('autostart')
    const search = nextParams.toString()
    navigate(`/notes/${id}${search ? `?${search}` : ''}`, { replace: true })
    void start()
  }, [autostart, id, micError, navigate, params, recordState, start])

  if (!full) {
    if (loadError) {
      return (
        <div className="flex h-full flex-col">
          <NoteHeader
            noteId={id}
            title=""
            recordState="idle"
            elapsedMs={0}
            onStart={() => {}}
            onStop={() => {}}
            disabledReason="Server unreachable"
            onTitleSaved={() => {}}
            onDeleteNote={() => {}}
            onDuplicate={() => {}}
            onExport={() => {}}
            pinned={false}
            onTogglePinned={() => {}}
            onExportSrt={() => {}}
            onExportAss={() => {}}
            onExportVtt={() => {}}
          />
          <div role="alert" className="p-8 text-muted-foreground">
            <p>Could not load note.</p>
            <Button
              variant="secondary"
              size="sm"
              className="mt-4"
              onClick={() => {
                setLoadError(false)
                setRetryCount((c) => c + 1)
              }}
            >
              Retry
            </Button>
          </div>
        </div>
      )
    }

    return (
    <div className="flex h-full flex-col">
      <NoteHeader
        noteId={id}
        title=""
        recordState="idle"
        elapsedMs={0}
        onStart={() => {}}
        onStop={() => {}}
        disabledReason="Still loading"
        onTitleSaved={() => {}}
        onDeleteNote={() => {}}
        onDuplicate={() => {}}
        onExport={() => {}}
        pinned={false}
        onTogglePinned={() => {}}
        onExportSrt={() => {}}
        onExportAss={() => {}}
        onExportVtt={() => {}}
      />
      <div className="p-8 space-y-4" data-testid="note-loading">
        <Skeleton className="h-8 w-2/3" />
        <div className="flex gap-2">
          <Skeleton className="h-5 w-24" />
          <Skeleton className="h-5 w-20" />
        </div>
        <div className="space-y-3 mt-4">
          {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-4 w-full" />)}
        </div>
      </div>
    </div>
    )
  }

  return (
    <div className="flex h-full flex-col">
      <NoteHeader
        autoFocusTitle={capture}
        noteId={id}
        title={full.note.title}
        recordState={recordState}
        elapsedMs={elapsedMs}
        micError={micError}
        onStart={start}
        onStop={stop}
        onDeviceChange={(deviceId) => {
          setSelectedDeviceId(deviceId)
          saveAudioPrefs({ deviceId, gain: gainLinear })
        }}
        onGainChange={(gain) => {
          setGainLinear(gain)
          saveAudioPrefs({ deviceId: selectedDeviceId, gain })
        }}
        recordingLevel={microphoneLevel}
        onMicRetry={() => { setMicError(null); void start() }}
        disabledReason={recordDisabledReason}
        onTitleSaved={(t) => {
          setFull((f) => (f ? { ...f, note: { ...f.note, title: t } } : f))
          refresh()
        }}
        onDeleteNote={() => setConfirmDelete(true)}
        onDuplicate={duplicateNote}
        onResummarize={reRunSummary}
        agentConfigured={agentConfigured !== false}
        onRetranscribe={full.note.status === 'ready' || full.note.status === 'failed' ? retranscribeNote : undefined}
        pinned={Boolean(full.note.pinned)}
        onTogglePinned={() => void togglePinned()}
        eventId={full.note.event_id}
        onLinkEvent={(eventId) => void linkEvent(eventId)}
        onUnlinkEvent={() => void unlinkEvent()}
        serverExportFormats={serverExportFormats}
        onExport={async () => {
          try {
            const md = fullNoteToMarkdown(full)
            const name = `${(full.note.title || 'note').replace(/[^\w\- ]+/g, '').trim() || 'note'}.md`
            await muesli.exportFile(name, md)
          } catch (err) { notify(err instanceof Error ? err.message : 'Could not export note', 'error') }
        }}
        onExportPlainText={async () => {
          try {
            const text = fullNoteToPlainText(full)
            const today = new Date().toISOString().slice(0, 10)
            const name = renderFilenameTemplate('{title} - {date}', {
              date: today,
              title: full.note.title || 'note',
            })
            await muesli.exportFile(`${name}.txt`, text)
          } catch (err) { notify(err instanceof Error ? err.message : 'Could not export note', 'error') }
        }}
        onExportSrt={async () => {
          try {
            const cues = buildSubtitleCues(full.transcript?.segments ?? [])
            const srt = cuesToSrt(cues)
            const name = `${(full.note.title || 'note').replace(/[^\w\- ]+/g, '').trim() || 'note'}.srt`
            await muesli.exportFile(name, srt)
          } catch (err) { notify(err instanceof Error ? err.message : 'Could not export subtitles', 'error') }
        }}
        onExportAss={async () => {
          try {
            const cues = buildSubtitleCues(full.transcript?.segments ?? [])
            const ass = cuesToAss(cues)
            const name = `${(full.note.title || 'note').replace(/[^\w\- ]+/g, '').trim() || 'note'}.ass`
            await muesli.exportFile(name, ass)
          } catch (err) { notify(err instanceof Error ? err.message : 'Could not export subtitles', 'error') }
        }}
        onExportVtt={async () => {
          try {
            const cues = buildSubtitleCues(full.transcript?.segments ?? [])
            const vtt = cuesToVtt(cues)
            const name = `${(full.note.title || 'note').replace(/[^\w\- ]+/g, '').trim() || 'note'}.vtt`
            await muesli.exportFile(name, vtt)
          } catch (err) { notify(err instanceof Error ? err.message : 'Could not export subtitles', 'error') }
        }}
        onExportWithOptions={() => setExportOptionsOpen(true)}
      />
      {confirmDelete && (
        <Dialog open onOpenChange={(o) => !o && setConfirmDelete(false)} title="Move to Trash?">
          <p className="text-sm text-muted-foreground">This moves the note to Trash. It&apos;s recoverable for 30 days, then permanently deleted. Any files you&apos;ve previously exported from this note (for example, a Markdown export) remain on your disk — Muesli will not remove them.</p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" size="sm" onClick={() => setConfirmDelete(false)}>Cancel</Button>
            <Button variant="destructive" size="sm" onClick={async () => {
              try { await muesli.deleteNote(id); refresh(); navigate('/notes') }
              catch (err) { notify(err instanceof Error ? err.message : 'Could not delete note', 'error'); setConfirmDelete(false) }
            }}>Move to Trash</Button>
          </div>
        </Dialog>
      )}
      <ExportOptionsDialog
        open={exportOptionsOpen}
        title="Export note"
        description="Choose a format and whether to include the transcript and speaker names."
        formats={[
          { value: 'md', label: 'Markdown' },
          { value: 'docx', label: 'Word' },
          { value: 'pdf', label: 'PDF' },
        ]}
        confirmLabel="Export note"
        onCancel={() => setExportOptionsOpen(false)}
        onConfirm={async (value) => {
          let failureMessage: string | null = null
          try {
            const result = await muesli.exportNote(id, value.format, {
              includeTranscript: value.includeTranscript,
              redactSpeakers: value.redactSpeakers,
            })
            if (!result.success && result.error !== 'cancelled') failureMessage = result.error
          } catch (err) {
            failureMessage = err instanceof Error ? err.message : 'Could not export note'
          }
          if (failureMessage) {
            notify(failureMessage, 'error')
            throw new Error(failureMessage)
          }
        }}
      />
      {pendingUpload && (
        <DuplicateAudioDialog
          existingNoteTitle={pendingUpload.existingNoteTitle}
          onOpenExisting={() => {
            navigate('/notes/' + pendingUpload.existingNoteId)
            setPendingUpload(null)
          }}
          onTranscribeAgain={async () => {
            const p = pendingUpload
            setPendingUpload(null)
            await doUpload(p.audio, p.mimeType)
          }}
        />
      )}
      <ProcessingBanner status={full.note.status} onRetry={retryPipeline} onProcessNext={processNext} statusEnteredAt={full.note.updated_at} onGetDownloadStatus={() => muesli.getDefaultTranscriberStatus()} />
      <LiveTranscriptPanel noteId={id} isRecording={recordState === 'recording'} />
      <TagBar
        tags={full.note.tags ?? []}
        suggestions={tagIndex(allNotes).map((t) => t.name)}
        onAdd={(name) => refreshAfterTagMutation(() => muesli.addTag(id, name), 'Could not add tag')}
        onRemove={(name) => refreshAfterTagMutation(() => muesli.removeTag(id, name), 'Could not remove tag')}
      />
      <FolderBar
        folders={folders}
        memberIds={full.note.folder_ids ?? []}
        onAdd={async (folderId) => {
          try { await muesli.addNoteFolder(id, folderId); setFull(await muesli.getFull(id)); refresh() }
          catch (err) { notify(err instanceof Error ? err.message : 'Could not add to folder', 'error') }
        }}
        onCreate={async (name) => {
          try { const f = await muesli.createFolder(name); await muesli.addNoteFolder(id, f.id); setFull(await muesli.getFull(id)); refresh() }
          catch (err) { notify(err instanceof Error ? err.message : 'Could not create folder', 'error') }
        }}
        onRemove={async (folderId) => {
          try { await muesli.removeNoteFolder(id, folderId); setFull(await muesli.getFull(id)); refresh() }
          catch (err) { notify(err instanceof Error ? err.message : 'Could not remove from folder', 'error') }
        }}
      />
      <div className="border-b border-border">
        <button
          type="button"
          aria-expanded={detailsOpen}
          aria-controls="note-details-panel"
          onClick={() => setDetailsOpen((o) => !o)}
          className="flex w-full items-center gap-2 px-6 py-3 text-left text-sm font-semibold uppercase tracking-wide text-muted-foreground hover:bg-muted/40"
        >
          <ChevronRight size={14} className={detailsOpen ? 'rotate-90 transition-transform' : 'transition-transform'} />
          Details
        </button>
        {detailsOpen && (
          <div id="note-details-panel">
            <NoteSharesPanel noteId={id} />
            <NoteLinksPanel
              noteId={id}
              allNotes={allNotes}
              onOpenNote={(noteId) => navigate(`/notes/${noteId}`)}
              refreshToken={linkRefreshToken}
            />
            <RelatedNotesPanel
              noteId={id}
              allNotes={allNotes}
              onOpenNote={(noteId) => navigate(`/notes/${noteId}`)}
              refreshToken={linkRefreshToken}
              onLinked={() => setLinkRefreshToken((value) => value + 1)}
            />
            <NoteActionItemsPanel noteId={id} />
          </div>
        )}
      </div>
      <div className="flex-1 overflow-y-auto px-6 py-4">
        {capture || full.note.status === 'recording' ? (
          <Suspense fallback={<div className="p-4 text-sm text-muted-foreground">Loading editor…</div>}>
            <NoteEditor initialMarkdown={full.body_markdown} onSave={(md) => muesli.updateBody(id, md)} />
          </Suspense>
        ) : (
          <>
            <div className="mb-2 flex justify-end">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                className="h-7 px-3 text-xs"
                disabled={transcriptSegments.length === 0}
                onClick={() => void handleCopyTranscript()}
              >
                {transcriptCopied ? 'Copied' : 'Copy transcript'}
              </Button>
            </div>
            <NoteView
              full={full}
              initialSegmentId={initialSegmentId}
              initialSegmentIndex={initialSegmentIndex}
              onSaveBody={(md) => muesli.updateBody(id, md)}
              onRenameSpeaker={async () => {
                try {
                  setFull(await muesli.getFull(id))
                  refresh()
                } catch (err) {
                  notify(err instanceof Error ? err.message : 'Could not refresh note', 'error')
                }
              }}
              templates={templates}
              onRegenerateTemplate={regenerateTemplate}
              regeneratingTemplateId={regeneratingTemplateId}
            />
          </>
        )}
      </div>
    </div>
  )
}
