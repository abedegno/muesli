import { useEffect, useState, type ReactNode } from 'react'
import { muesli } from '@/api'
import type { EmbeddedStartupStatus } from '../../shared/types'

function formatPercent(percent?: number | null): string | null {
  if (typeof percent !== 'number' || !Number.isFinite(percent)) return null
  return `${Math.max(0, Math.min(100, Math.round(percent)))}%`
}

function describeProgress(status: Extract<EmbeddedStartupStatus, { status: 'progress' }> | null): string {
  if (!status) return 'Starting...'

  switch (status.phase) {
    case 'db-init':
    case 'pgvector':
    case 'migrate':
      return 'Initializing database...'
    case 'ollama-check':
      return 'Checking for Ollama...'
    case 'model-pull': {
      const percent = formatPercent(status.percent)
      return percent ? `Downloading models... ${percent}` : 'Downloading models...'
    }
    default:
      return status.phase ? `Starting ${status.phase}...` : 'Starting...'
  }
}

function StartupBanner({ onDismiss }: { onDismiss: () => void }) {
  return (
    <div className="border-b border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-950 dark:text-amber-100">
      <div className="mx-auto flex max-w-5xl items-start gap-3">
        <div className="min-w-0 flex-1">
          <p className="font-medium">Install Ollama to enable summaries & search.</p>
          <p className="mt-1 text-amber-900/80 dark:text-amber-100/75">
            <a className="underline underline-offset-2 hover:no-underline" href="https://ollama.com/download" target="_blank" rel="noreferrer">
              Download Ollama
            </a>
            {' – '}
            <a
              className="underline underline-offset-2 hover:no-underline"
              href="https://github.com/abedegno/muesli/blob/main/docs/DESKTOP-ONBOARDING.md"
              target="_blank"
              rel="noreferrer"
            >
              Learn more
            </a>
          </p>
        </div>
        <button
          type="button"
          onClick={onDismiss}
          className="rounded px-2 py-1 text-xs font-medium uppercase tracking-wide text-amber-950/80 hover:bg-amber-500/10 dark:text-amber-50/80"
        >
          Dismiss
        </button>
      </div>
    </div>
  )
}

function StartupPanel({
  title,
  detail,
  percent,
}: {
  title: string
  detail?: string | null
  percent?: number | null
}) {
  const percentLabel = formatPercent(percent)
  const progressWidth = percentLabel ? `${Math.max(0, Math.min(100, Math.round(percent ?? 0)))}%` : '20%'

  return (
    <div className="flex h-full min-h-0 flex-col bg-app-chrome text-foreground">
      <main className="flex flex-1 items-center justify-center px-6 py-10">
        <section className="w-full max-w-xl rounded-[var(--radius)] border border-border bg-card px-8 py-7 shadow-md">
          <p className="text-xs font-semibold uppercase tracking-[0.25em] text-muted-foreground">Starting Muesli</p>
          <h1 className="mt-3 text-2xl font-semibold tracking-tight">{title}</h1>
          {detail ? <p className="mt-2 text-sm leading-6 text-muted-foreground">{detail}</p> : null}
          <div className="mt-6 h-2 overflow-hidden rounded-full bg-muted">
            <div className="h-full rounded-full bg-foreground/80 transition-[width] duration-300" style={{ width: progressWidth }} />
          </div>
          <p className="mt-3 text-sm text-muted-foreground">
            {percentLabel ?? 'Working...'}
          </p>
        </section>
      </main>
    </div>
  )
}

function StartupErrorScreen({ message, logPath }: { message: string; logPath: string }) {
  return (
    <div className="flex h-full min-h-0 flex-col bg-app-chrome text-foreground">
      <main className="flex flex-1 items-center justify-center px-6 py-10">
        <section className="w-full max-w-2xl rounded-[var(--radius)] border border-destructive/30 bg-card px-8 py-7 shadow-md">
          <p className="text-xs font-semibold uppercase tracking-[0.25em] text-muted-foreground">Startup failed</p>
          <h1 className="mt-3 text-2xl font-semibold tracking-tight">Muesli could not start</h1>
          <p className="mt-3 text-sm leading-6 text-foreground">{message}</p>
          <div className="mt-6 space-y-2 text-sm">
            <p className="font-medium text-muted-foreground">Log file</p>
            <code className="block select-text rounded bg-muted px-3 py-2 font-mono text-xs leading-6 break-all text-foreground">{logPath}</code>
          </div>
        </section>
      </main>
    </div>
  )
}

export function EmbeddedStartupGate({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<EmbeddedStartupStatus | null>(null)
  const [bannerDismissed, setBannerDismissed] = useState(false)
  const hasStartupBridge = typeof muesli.onEmbeddedStartupStatus === 'function'

  useEffect(() => {
    if (!hasStartupBridge) return
    const unsubscribe = muesli.onEmbeddedStartupStatus?.((next) => {
      setStatus(next)
    })
    return unsubscribe
  }, [hasStartupBridge])

  if (!hasStartupBridge) return <>{children}</>

  const degraded = status?.status === 'progress' || status?.status === 'ready' ? status.degraded : false

  if (status?.status === 'error') {
    return <StartupErrorScreen message={status.message} logPath={status.logPath} />
  }

  const title = describeProgress(status?.status === 'progress' ? status : null)
  const detail = status?.status === 'progress' ? status.detail : null
  const percent = status?.status === 'progress' ? status.percent : null

  return (
    <div className="flex h-screen min-h-screen flex-col overflow-hidden">
      {degraded && !bannerDismissed ? (
        <div className="shrink-0">
          <StartupBanner onDismiss={() => setBannerDismissed(true)} />
        </div>
      ) : null}
      <div className="min-h-0 flex-1">
        {status?.status === 'ready' ? (
          <>{children}</>
        ) : (
          <StartupPanel title={title} detail={detail} percent={percent} />
        )}
      </div>
    </div>
  )
}
