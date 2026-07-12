import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams, useOutletContext } from 'react-router-dom'
import { muesli } from '@/api'
import { RecordingSession } from '../../main/recorder'
import { ElectronCapture } from '../capture/electronCapture'
import { pollNote } from '@/lib/pollNote'
import { isProcessing } from '@/lib/status'
import { useToast } from '@/components/ui/Toast'
import { useActivity } from '@/lib/activityStore'
import { NoteHeader } from './NoteHeader'
import { ProcessingBanner } from './ProcessingBanner'
import { TagBar } from './TagBar'
import { FolderBar } from './FolderBar'
import { LiveTranscriptPanel } from './LiveTranscriptPanel'
import { tagIndex } from '@/lib/tagIndex'
import { loadAudioPrefs, saveAudioPrefs } from '@/lib/audioPrefs'
// Lazy so TipTap (the renderer-bundle bulk) is only fetched when an editor mounts.
const NoteEditor = lazy(() => import('./NoteEditor').then((m) => ({ default: m.NoteEditor })))
import { NoteView } from './NoteView'
import { fullNoteToMarkdown, fullNoteToPlainText } from '@/lib/noteMarkdown'
import { renderFilenameTemplate } from '@/lib/filenameTemplate'
import { buildSubtitleCues } from '@/lib/subtitleCues'
import { cuesToSrt, cuesToAss, cuesToVtt } from '@/lib/subtitleFormats'
import { Dialog } from '@/components/ui/Dialog'
import { Button } from '@/components/ui/Button'
import type { FullNote, Template } from '../../shared/types'
import { isTerminal } from '../../shared/types'
import { Skeleton } from './ui/Skeleton'
import type { RecordState } from './RecordControl'
import { DuplicateAudioDialog } from './DuplicateAudioDialog'

interface Ctx { notes: import('../../shared/types').Note[]; allNotes: import('../../shared/types').Note[]; folders: import('../../shared/types').Folder[]; refresh: () => void }

export function NoteScreen() {
  const { id = '' } = useParams()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { allNotes, folders, refresh } = useOutletContext<Ctx>()
  const { notify } = useToast()
  const { addUpload, updateUpload, addProcessing, updateProcessing } = useActivity()

  const [full, setFull] = useState<FullNote | null>(null)
  const [templates, setTemplates] = useState<Template[]>([])
  const [regeneratingTemplateId, setRegeneratingTemplateId] = useState<string | null>(null)
  const [loadError, setLoadError] = useState(false)
  const [retryCount, setRetryCount] = useState(0)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [recordState, setRecordState] = useState<RecordState>('idle')
  const [elapsedMs, setElapsedMs] = useState(0)
  const [micError, setMicError] = useState<Error | null>(null)
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
  const mountedRef = useRef(true)
  // Stores the onUploadProgress unsubscribe function; cleaned up on unmount.
  const uploadProgressUnsubRef = useRef<(() => void) | null>(null)

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
    return () => {
      tagAbortRef.current?.abort()
    }
  }, [id])

  const retryPipeline = useCallback(async () => {
    if (!id) return
    try {
      await muesli.retryNote(id)
      refresh()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not retry the pipeline', 'error')
    }
  }, [id, refresh, notify])

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
      notify(err instanceof Error ? err.message : 'Could not re-run the summary', 'error')
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
      void muesli.startNoteStream?.(id)?.catch(() => {})
      const session = new RecordingSession(
        new ElectronCapture({
          deviceId: selectedDeviceId,
          gainLinear,
          onPcmFrame: (frame) => {
            void muesli.sendNoteStreamAudio?.(id, frame)?.catch(() => {})
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
  }, [gainLinear, id, notify, selectedDeviceId])

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

  const exportServerNote = useCallback(async (format: string) => {
    try {
      const result = await muesli.exportNote(id, format)
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

  const recordDisabledReason: string | undefined = loadError ? 'Server unreachable' : undefined

  const capture = params.get('capture') === '1'
  const autostart = params.get('autostart') === '1'
  const initialSegmentId = params.get('segment') || undefined
  // Set by a global-chat citation chip's navigation (ChatScreen's
  // onCiteClick) -- already a resolved transcript-segment array index, so no
  // id lookup is needed (see NoteView's `initialSegmentIndex` prop).
  const rawSegmentIndex = params.get('segment_index')
  const parsedSegmentIndex = rawSegmentIndex != null ? Number(rawSegmentIndex) : NaN
  const initialSegmentIndex = Number.isFinite(parsedSegmentIndex) ? parsedSegmentIndex : undefined

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
        onMicRetry={() => { setMicError(null); void start() }}
        disabledReason={recordDisabledReason}
        onTitleSaved={(t) => {
          setFull((f) => (f ? { ...f, note: { ...f.note, title: t } } : f))
          refresh()
        }}
        onDeleteNote={() => setConfirmDelete(true)}
        onDuplicate={duplicateNote}
        onResummarize={reRunSummary}
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
      />
      {confirmDelete && (
        <Dialog open onOpenChange={(o) => !o && setConfirmDelete(false)} title="Move to Trash?">
          <p className="text-sm text-muted-foreground">This moves the note to Trash. It&apos;s recoverable for 30 days, then permanently deleted. Any files you&apos;ve previously exported from this note (for example, a Markdown export) remain on your disk — Muesli will not remove them.</p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" size="sm" onClick={() => setConfirmDelete(false)}>Cancel</Button>
            <Button variant="destructive" size="sm" onClick={async () => {
              try { await muesli.deleteNote(id); refresh(); navigate('/') }
              catch (err) { notify(err instanceof Error ? err.message : 'Could not delete note', 'error'); setConfirmDelete(false) }
            }}>Move to Trash</Button>
          </div>
        </Dialog>
      )}
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
      <div className="flex-1 overflow-y-auto px-6 py-4">
        {capture || full.note.status === 'recording' ? (
          <Suspense fallback={<div className="p-4 text-sm text-muted-foreground">Loading editor…</div>}>
            <NoteEditor initialMarkdown={full.body_markdown} onSave={(md) => muesli.updateBody(id, md)} />
          </Suspense>
        ) : (
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
        )}
      </div>
    </div>
  )
}
