import { uiText } from '@/i18n'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { Dialog } from '@/components/ui/Dialog'
import { formatDate } from '@/i18n'
import type { RecordingRecoveryInfo } from '../../shared/types'

export function RecoveryInbox() {
  const navigate = useNavigate()
  const [items, setItems] = useState<Array<RecordingRecoveryInfo & { title: string }>>([])
  const [open, setOpen] = useState(false)
  const [discard, setDiscard] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    let active = true,
      request = 0
    const load = async () => {
      if (typeof muesli.listRecordingRecoveries !== 'function') return
      const generation = ++request
      try {
        const saved = await muesli.listRecordingRecoveries()
        const titled = await Promise.all(
          saved.slice(0, 20).map(async (item) => {
            const note = await muesli.getFull(item.noteId).catch(() => null)
            return { ...item, title: note?.note.title || uiText('Recovered recording') }
          })
        )
        if (active && generation === request) setItems(titled)
      } catch {
        /* A failed check must not claim that audio was discarded. */
      }
    }
    void load()
    const update = () => void load()
    window.addEventListener('muesli:notes-changed', update)
    document.addEventListener('visibilitychange', update)
    return () => {
      active = false
      window.removeEventListener('muesli:notes-changed', update)
      document.removeEventListener('visibilitychange', update)
    }
  }, [])
  async function action(id: string, kind: 'retry' | 'export' | 'discard') {
    setBusy(id)
    setError(null)
    try {
      if (kind === 'retry') {
        await muesli.retryRecordingUpload(id)
        setItems((current) => current.filter((item) => item.noteId !== id))
        window.dispatchEvent(new Event('muesli:notes-changed'))
      }
      if (kind === 'export') {
        const result = await muesli.exportRecordingRecovery(id)
        if (!result.success && result.error !== 'cancelled')
          throw new Error(result.error || uiText('Could not save the audio copy.'))
      }
      if (kind === 'discard') {
        await muesli.discardRecordingRecovery(id)
        setItems((current) => current.filter((item) => item.noteId !== id))
        setDiscard(null)
      }
    } catch (error) {
      setError(
        error instanceof Error
          ? error.message
          : 'Could not recover recording. Your saved audio has been kept.'
      )
    } finally {
      setBusy(null)
    }
  }
  if (!items.length) return null
  return (
    <>
      <div
        role="status"
        className="flex flex-wrap items-center justify-between gap-2 border-b border-border bg-muted/40 px-6 py-3 text-sm"
      >
        <span>
          {items.length === 1
            ? uiText('A saved recording needs your attention.')
            : uiText('{value1} saved recordings need your attention.', { value1: items.length })}
        </span>
        <Button size="sm" variant="secondary" onClick={() => setOpen(true)}>
          {uiText('Review recordings')}
        </Button>
      </div>
      <Dialog open={open} onOpenChange={setOpen} title={uiText('Saved recordings')}>
        <p className="mb-4 text-sm text-muted-foreground">
          {uiText(
            'Audio recovered on this device stays here until you upload or discard it. Only recordings for the connected account are shown. Save a copy before discarding anything you need.'
          )}
        </p>
        <div className="max-h-[55vh] space-y-4 overflow-y-auto">
          {items.map((item) => (
            <section key={item.noteId} className="space-y-2 rounded-lg border border-border p-3">
              <h2 className="font-medium">{item.title}</h2>
              <p className="text-sm text-muted-foreground">
                {formatDate(new Date(item.createdAt), { dateStyle: 'medium', timeStyle: 'short' })}{' '}
                {uiText('·')} {(item.byteLength / 1024 / 1024).toFixed(1)} {uiText('MB')}
              </p>
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  disabled={!!busy}
                  onClick={() => void action(item.noteId, 'retry')}
                >
                  {busy === item.noteId ? uiText('Working…') : uiText('Retry upload')}
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={!!busy}
                  onClick={() => void action(item.noteId, 'export')}
                >
                  {uiText('Save audio copy')}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setOpen(false)
                    navigate(`/notes/${item.noteId}`)
                  }}
                >
                  {uiText('Open note')}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={!!busy}
                  onClick={() => setDiscard(item.noteId)}
                >
                  {uiText('Discard…')}
                </Button>
              </div>
            </section>
          ))}
        </div>
        {error && (
          <p role="alert" className="mt-3 text-sm text-destructive">
            {error}
          </p>
        )}
        <Button className="mt-4" variant="secondary" onClick={() => setOpen(false)}>
          {uiText('Close')}
        </Button>
      </Dialog>
      <Dialog
        open={!!discard}
        onOpenChange={(value) => {
          if (!value) setDiscard(null)
        }}
        title={uiText('Discard saved audio?')}
      >
        <p className="text-sm">
          {uiText(
            'This permanently removes the local recovery audio. Your written notes stay available. Save an audio copy first if you need it.'
          )}
        </p>
        <div className="mt-4 flex gap-2">
          <Button variant="secondary" onClick={() => setDiscard(null)}>
            {uiText('Cancel')}
          </Button>
          <Button
            variant="destructive"
            disabled={!!busy}
            onClick={() => {
              if (discard) void action(discard, 'discard')
            }}
          >
            {uiText('Discard audio')}
          </Button>
        </div>
      </Dialog>
    </>
  )
}
