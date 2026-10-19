import { uiText } from '@/i18n'
import { useEffect, useState } from 'react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { Select } from '@/components/ui/Select'
import { Dialog } from '@/components/ui/Dialog'
import {
  TRANSCRIPTION_LANGUAGES,
  type TranscriptionModelState,
} from '../../shared/transcriptionModels'

export function TranscriptionModels({ bridge = muesli }: { bridge?: Partial<typeof muesli> }) {
  const [state, setState] = useState<TranscriptionModelState | null>(null)
  const [chosen, setChosen] = useState('tiny.en')
  const [language, setLanguage] = useState('en')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState(false)
  useEffect(() => {
    let active = true,
      timer: ReturnType<typeof setTimeout> | undefined,
      initialized = false
    const deadline = Date.now() + 30 * 60_000
    if (typeof bridge.getTranscriptionModels !== 'function') return
    const check = async () => {
      try {
        const next = await bridge.getTranscriptionModels!()
        if (!active) return
        setState(next)
        if (!initialized) {
          initialized = true
          setChosen(next.selected)
          setLanguage(next.language)
        }
        if (Date.now() < deadline)
          timer = setTimeout(() => {
            void check()
          }, 2000)
      } catch {
        if (active) setError('Could not load local models. Reopen Settings to try again.')
      }
    }
    void check()
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [bridge])
  if (!state)
    return error ? (
      <p role="alert" className="text-sm text-destructive">
        {uiText(error)}
      </p>
    ) : null
  const selected = state.models.find((model) => model.id === chosen)
  const downloading = state.download?.status === 'downloading'
  return (
    <section
      className="mt-4 space-y-3 rounded-xl border border-border p-4"
      aria-labelledby="local-model-heading"
    >
      <h3 id="local-model-heading" className="text-sm font-medium">
        {uiText('Language and local transcription model')}
      </h3>
      <p className="text-sm leading-6 text-muted-foreground">
        {uiText(
          'The included model is English-only. Choose a multilingual model for other spoken languages. This changes transcription on this device, separately from the app’s display language. Larger models use more memory and may take longer.'
        )}
      </p>
      <label className="flex flex-col gap-1 text-sm">
        {uiText('Local model')}
        <Select
          aria-label={uiText('Local model')}
          value={chosen}
          disabled={busy || downloading}
          onChange={(event) => {
            setChosen(event.target.value)
            setLanguage(event.target.value === 'tiny.en' ? 'en' : 'auto')
          }}
        >
          {state.models.map((model) => (
            <option key={model.id} value={model.id}>
              {model.name} {uiText('—')}{' '}
              {model.multilingual ? uiText('Multilingual') : uiText('English only')}
              {model.installed
                ? uiText(' (installed)')
                : uiText(' ({value1} MB download)', {
                    value1: Math.ceil(model.bytes / 1024 / 1024),
                  })}
            </option>
          ))}
        </Select>
      </label>
      {selected && (
        <p className="text-xs leading-5 text-muted-foreground">
          {uiText('About')} {selected.memoryMB}{' '}
          {uiText(
            'MB of model memory per transcription service. Live and final transcription may load separate copies. CPU and Apple Silicon are supported; performance depends on your device.'
          )}
        </p>
      )}
      <label className="flex flex-col gap-1 text-sm">
        {uiText('Spoken language')}
        <Select
          disabled={busy || downloading || !selected?.multilingual}
          aria-label={uiText('Spoken language')}
          value={language}
          onChange={(event) => setLanguage(event.target.value)}
        >
          {TRANSCRIPTION_LANGUAGES.map((language) => (
            <option key={language.id} value={language.id}>
              {language.id === 'auto' ? uiText('Detect automatically') : language.name}
            </option>
          ))}
        </Select>
      </label>
      {downloading && (
        <div role="status" className="space-y-2 text-sm">
          <p>
            {uiText('Downloading model…')} {state.download?.percent}
            {uiText('%')}
          </p>
          <progress
            className="w-full"
            aria-label={uiText('Transcription model download')}
            value={state.download?.percent ?? 0}
            max={100}
          />
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void bridge.cancelTranscriptionModelDownload?.()}
          >
            {uiText('Cancel download')}
          </Button>
        </div>
      )}
      {state.download?.status === 'failed' && (
        <p role="alert" className="text-sm text-destructive">
          {uiText(state.download.error ?? '')}
        </p>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {uiText(error)}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        {!selected?.installed && (
          <Button
            disabled={busy || downloading}
            onClick={() => {
              setBusy(true)
              setError(null)
              void Promise.resolve(bridge.installTranscriptionModel?.(chosen))
                .then(() => bridge.getTranscriptionModels!())
                .then(setState)
                .catch((error) =>
                  setError(
                    error instanceof Error ? error.message : 'Could not download this model.'
                  )
                )
                .finally(() => setBusy(false))
            }}
          >
            {uiText('Download selected model')}
          </Button>
        )}
        {selected?.installed && (
          <Button
            variant="secondary"
            disabled={
              busy || downloading || (state.selected === chosen && state.language === language)
            }
            onClick={() => setConfirm(true)}
          >
            {uiText('Use model and restart…')}
          </Button>
        )}
      </div>
      <p className="text-xs leading-5 text-muted-foreground">
        {uiText(
          'Model downloads come from the upstream Whisper model repository. Your meeting audio is not sent there.'
        )}
      </p>
      <Dialog
        open={confirm}
        onOpenChange={setConfirm}
        title={uiText('Restart with this transcription model?')}
      >
        <p className="text-sm">
          {uiText(
            'Finish and save any recording first. Muesli will restart with your chosen model and spoken language. Your notes and recordings stay in this profile.'
          )}
        </p>
        <div className="mt-4 flex gap-2">
          <Button variant="secondary" onClick={() => setConfirm(false)}>
            {uiText('Cancel')}
          </Button>
          <Button
            disabled={busy}
            onClick={() => {
              setBusy(true)
              setError(null)
              void Promise.resolve(bridge.activateTranscriptionModel?.(chosen, language)).catch(
                (error) => {
                  setError(error instanceof Error ? error.message : 'Could not change models.')
                  setBusy(false)
                  setConfirm(false)
                }
              )
            }}
          >
            {busy ? uiText('Restarting…') : uiText('Restart Muesli')}
          </Button>
        </div>
      </Dialog>
    </section>
  )
}
