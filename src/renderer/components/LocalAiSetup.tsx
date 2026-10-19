import { uiText } from '@/i18n'
import { useEffect, useState } from 'react'
import { Download, ExternalLink, RotateCw } from 'lucide-react'
import { Button } from '@/components/ui/Button'
import type { LocalOllamaStatus, MuesliBridge } from '../../shared/ipc'

export type LocalAiSetupBridge = Pick<
  MuesliBridge,
  'platform' | 'getLocalOllamaStatus' | 'downloadOllama' | 'openOllama' | 'finishOllamaSetup'
>

export function LocalAiSetup({ bridge }: { bridge: LocalAiSetupBridge }) {
  const [status, setStatus] = useState<LocalOllamaStatus | null>(null)
  const [probeFailed, setProbeFailed] = useState(false)
  const [paused, setPaused] = useState(false)
  const [retry, setRetry] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState<'download' | 'open' | 'finish' | null>(null)

  useEffect(() => {
    let active = true
    let timer: ReturnType<typeof setTimeout> | undefined
    const deadline = Date.now() + 5 * 60_000
    setPaused(false)
    const check = async () => {
      let reachable = false
      if (document.visibilityState !== 'hidden') {
        try {
          const next = await bridge.getLocalOllamaStatus()
          reachable = next.reachable
          if (active) {
            setStatus(next)
            setProbeFailed(false)
          }
        } catch {
          if (active) {
            setStatus(null)
            setProbeFailed(true)
          }
        }
      }
      if (!active || reachable) return
      if (Date.now() >= deadline) {
        setPaused(true)
        return
      }
      timer = setTimeout(() => {
        void check()
      }, 2000)
    }
    void check()
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [bridge, retry])

  async function perform(action: 'download' | 'open' | 'finish') {
    setBusy(action)
    setError(null)
    try {
      if (action === 'download') await bridge.downloadOllama()
      else if (action === 'open') await bridge.openOllama()
      else await bridge.finishOllamaSetup()
      if (action !== 'finish') {
        setBusy(null)
        setRetry((n) => n + 1)
      }
    } catch (err) {
      setError(
        err instanceof Error ? err.message : 'Could not complete this step. Please try again.'
      )
      setBusy(null)
    }
  }

  return (
    <div className="space-y-4">
      <p role="status" className="text-sm leading-6 text-muted-foreground">
        {status?.reachable
          ? uiText('Ollama is running. Let’s connect it to Muesli.')
          : probeFailed
            ? uiText('Could not check Ollama. You can retry or finish setup later.')
            : status?.appInstalled
              ? uiText('Ollama is installed. Open it to continue.')
              : uiText('Install Ollama to add local AI summaries and semantic search.')}
      </p>
      {status?.reachable ? (
        <div className="space-y-3 rounded-xl border border-primary/20 bg-primary/10 p-4">
          <p className="text-sm leading-6">
            {uiText(
              'Muesli will restart and download its recommended summary and search models. Allow around 3 GB of disk space and keep your internet connection on during setup.'
            )}
          </p>
          <Button disabled={busy !== null} onClick={() => void perform('finish')}>
            <RotateCw aria-hidden="true" className="h-4 w-4" />
            {busy === 'finish' ? uiText('Restarting…') : uiText('Restart and set up AI')}
          </Button>
        </div>
      ) : (
        <div className="space-y-3">
          {bridge.platform === 'darwin' ? (
            <>
              <p className="text-sm leading-6 text-muted-foreground">
                {uiText(
                  'Open the downloaded installer, drag Ollama to Applications, then open Ollama. Muesli will detect it automatically.'
                )}
              </p>
              <Button
                variant="secondary"
                disabled={busy !== null}
                onClick={() => void perform(status?.appInstalled ? 'open' : 'download')}
              >
                {status?.appInstalled ? (
                  <ExternalLink aria-hidden="true" className="h-4 w-4" />
                ) : (
                  <Download aria-hidden="true" className="h-4 w-4" />
                )}
                {busy === 'download'
                  ? uiText('Opening download…')
                  : busy === 'open'
                    ? uiText('Opening Ollama…')
                    : status?.appInstalled
                      ? uiText('Open Ollama')
                      : uiText('Download Ollama for Mac')}
              </Button>
            </>
          ) : (
            <>
              <Button
                variant="secondary"
                disabled={busy !== null}
                onClick={() => void perform('download')}
              >
                {uiText('Download Ollama')}
              </Button>
              {bridge.platform === 'linux' && (
                <code className="block overflow-x-auto rounded bg-muted px-3 py-2 text-xs">
                  curl -fsSL https://ollama.com/install.sh | sh
                </code>
              )}
              <p className="text-sm text-muted-foreground">
                {uiText('Install and open Ollama. Muesli will detect it automatically.')}
              </p>
            </>
          )}
        </div>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {!status?.reachable && (
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="ghost" disabled={busy !== null} onClick={() => setRetry((n) => n + 1)}>
            {uiText('Re-check Ollama')}
          </Button>
          {paused && (
            <p className="text-xs text-muted-foreground">
              {uiText('Automatic checks paused. Re-check when Ollama is open.')}
            </p>
          )}
        </div>
      )}
    </div>
  )
}
