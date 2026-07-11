import { useState, useEffect, useRef } from 'react'
import { Button } from '@/components/ui/Button'
import { useAnnouncer } from '@/hooks/useAnnouncer'
import { statusLabel } from '@/lib/status'
import type { NoteStatus, PluginStatus } from '../../shared/types'

const PROCESSING_STATUSES: NoteStatus[] = ['uploaded', 'transcribing', 'summarizing']

export function ProcessingBanner({
  status,
  onRetry,
  onProcessNext,
  statusEnteredAt,
  onGetDownloadStatus,
}: {
  status: NoteStatus
  onRetry?: () => void
  /** Bumps this note's still-pending job(s) to the front of the queue ("process next"). */
  onProcessNext?: () => void
  statusEnteredAt?: string
  /** Optional callback polled every 3 s while status === 'transcribing'. */
  onGetDownloadStatus?: () => Promise<PluginStatus>
}) {
  const [elapsedSec, setElapsedSec] = useState(0)
  const [downloadStatus, setDownloadStatus] = useState<PluginStatus | null>(null)
  const { announce } = useAnnouncer()
  const mountedRef = useRef(false)
  const prevStatusRef = useRef<NoteStatus>(status)

  // Announce status changes (skip initial mount)
  useEffect(() => {
    if (!mountedRef.current) {
      mountedRef.current = true
      prevStatusRef.current = status
      return
    }
    if (prevStatusRef.current === status) return
    prevStatusRef.current = status
    announce(statusLabel(status))
  }, [status, announce])

  useEffect(() => {
    if (!statusEnteredAt || !PROCESSING_STATUSES.includes(status)) {
      setElapsedSec(0)
      return
    }

    const computeElapsed = () =>
      Math.floor((Date.now() - new Date(statusEnteredAt).getTime()) / 1000)

    setElapsedSec(computeElapsed())

    const id = setInterval(() => setElapsedSec(computeElapsed()), 1000)
    return () => clearInterval(id)
  }, [statusEnteredAt, status])

  // Poll the plugin's download status every 3 s while transcribing.
  useEffect(() => {
    if (status !== 'transcribing' || !onGetDownloadStatus) {
      setDownloadStatus(null)
      return
    }

    let cancelled = false

    const poll = async () => {
      try {
        const s = await onGetDownloadStatus()
        if (!cancelled) setDownloadStatus(s)
      } catch {
        // best-effort — silently ignore
      }
    }

    void poll()
    const id = setInterval(() => { void poll() }, 3000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [status, onGetDownloadStatus])

  if (status === 'failed') {
    return (
      <div role="alert" className="flex items-center justify-between gap-3 border-b border-border bg-destructive/10 px-6 py-2 text-sm text-destructive">
        <span>Processing failed.</span>
        {onRetry && <Button size="sm" variant="secondary" onClick={onRetry}>Re-run</Button>}
      </div>
    )
  }
  if (status === 'transcribing' || status === 'summarizing' || status === 'uploaded') {
    const label = status === 'summarizing' ? 'Summarizing…' : status === 'transcribing' ? 'Transcribing…' : 'Uploaded — queued…'
    const downloadBanner =
      downloadStatus?.status === 'downloading' && downloadStatus.model
        ? `Downloading model ${downloadStatus.model} (${downloadStatus.percent ?? 0}%)`
        : null
    return (
      <div role="status" aria-live="polite" className="flex flex-col border-b border-border bg-accent/10 px-6 py-2 text-sm text-accent">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <span className="h-3 w-3 animate-spin rounded-full border-2 border-accent border-t-transparent" aria-hidden />
            {statusEnteredAt ? `${label} ${elapsedSec} s` : label}
          </div>
          {onProcessNext && (
            <Button size="sm" variant="secondary" onClick={onProcessNext}>Process next</Button>
          )}
        </div>
        {downloadBanner && (
          <span className="mt-1 text-xs text-muted-foreground" data-testid="download-progress-banner">
            {downloadBanner}
          </span>
        )}
      </div>
    )
  }
  return null
}
