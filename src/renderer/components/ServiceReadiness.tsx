import { uiText } from '@/i18n'
import { useEffect, useState } from 'react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import type { ServiceReadinessSnapshot } from '../../shared/readiness'

export function ServiceReadiness({ bridge = muesli }: { bridge?: Partial<typeof muesli> }) {
  const [snapshot, setSnapshot] = useState<ServiceReadinessSnapshot | null>(null)
  const [admin, setAdmin] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [refresh, setRefresh] = useState(0)
  useEffect(() => {
    let active = true,
      timer: ReturnType<typeof setTimeout> | undefined
    const deadline = Date.now() + 30 * 60_000
    if (typeof bridge.getServiceReadiness !== 'function') return
    void Promise.resolve(bridge.getCapabilities?.())
      .then((caps) => {
        if (active) setAdmin(caps?.isAdmin === true)
      })
      .catch(() => {})
    const check = async () => {
      try {
        const next = await bridge.getServiceReadiness!()
        if (!active) return
        setSnapshot(next)
        setError(null)
        if (next.models.some((model) => model.status === 'downloading') && Date.now() < deadline) {
          timer = setTimeout(() => {
            void check()
          }, 5000)
        }
      } catch {
        if (active)
          setError(
            'Could not check the connected server. Your recording and notes are still available; try checking again.'
          )
      }
    }
    void check()
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [refresh, bridge])
  if (!snapshot && !error) return null
  const labels: Record<string, string> = {
    transcriber: 'Final transcript',
    'streaming-transcriber': 'Live transcript',
    agent: 'AI summaries and chat',
    'semantic-search': 'Semantic search',
  }
  return (
    <section
      className="my-4 space-y-3 rounded-xl border border-border p-4"
      aria-label={uiText('Connected server readiness')}
    >
      <h3 className="text-sm font-medium">{uiText('Connected server readiness')}</h3>
      <p className="text-xs leading-5 text-muted-foreground">
        {uiText(
          'These checks use your connected server. AI is optional; a model download does not prevent recording or writing notes.'
        )}
      </p>
      {snapshot && (
        <ul className="space-y-2 text-sm">
          {snapshot.services.map((service) => (
            <li key={service.id} className="flex flex-wrap justify-between gap-2">
              <span>{uiText(labels[service.id] ?? service.id)}</span>
              <span>
                {!service.configured
                  ? uiText('Not configured')
                  : service.healthy
                    ? uiText('Connection available')
                    : uiText('Connection needs attention')}
              </span>
            </li>
          ))}
        </ul>
      )}
      {snapshot?.models.map((model) => (
        <div key={model.name} className="space-y-1 text-sm" role="status">
          <p>
            {model.name} {uiText('—')}{' '}
            {model.status === 'ready'
              ? uiText('Downloaded')
              : model.status === 'failed'
                ? uiText('Download failed')
                : uiText('Downloading… {value1}%', { value1: model.percent })}
          </p>
          {model.status === 'downloading' && (
            <progress
              className="w-full"
              aria-label={uiText('{value1} download', { value1: model.name })}
              value={model.percent}
              max={100}
            />
          )}
          {model.status === 'failed' && (
            <p className="text-xs text-muted-foreground">
              {(model.error && uiText(model.error)) ||
                uiText('Check your connection, free disk space and model service, then retry.')}
            </p>
          )}
        </div>
      ))}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {uiText(error)}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="secondary" onClick={() => setRefresh((value) => value + 1)}>
          {uiText('Re-check services')}
        </Button>
        {admin &&
          snapshot?.managed_models &&
          snapshot.models.some((model) => model.status === 'failed') && (
            <Button
              size="sm"
              disabled={busy}
              onClick={() => {
                setBusy(true)
                void Promise.resolve(bridge.retryAIModels?.())
                  .then(() => setRefresh((value) => value + 1))
                  .catch((error) =>
                    setError(
                      error instanceof Error ? error.message : 'Could not retry model downloads.'
                    )
                  )
                  .finally(() => setBusy(false))
              }}
            >
              {busy ? uiText('Starting download…') : uiText('Retry AI model downloads')}
            </Button>
          )}
      </div>
    </section>
  )
}
