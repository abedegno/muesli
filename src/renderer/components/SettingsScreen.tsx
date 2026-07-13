import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { loadCalendarPrefs, saveCalendarPrefs } from '@/lib/calendarPrefs'
import type { DigestConfig } from '../../shared/types'

type Theme = 'system' | 'light' | 'dark'
type HealthState = { status: 'idle' } | { status: 'checking' } | { status: 'connected'; version?: string } | { status: 'unreachable' }

function healthUrlFor(serverUrl: string): string {
  return new URL('healthz', `${serverUrl.replace(/\/+$/, '')}/`).toString()
}

function versionFromHealth(data: Record<string, unknown>): string | undefined {
  const value = data.version ?? data.serverVersion ?? data.appVersion
  if (typeof value === 'string') return value.trim() || undefined
  if (typeof value === 'number') return String(value)
  return undefined
}

export function SettingsScreen({ onDisconnected }: { onDisconnected: () => void }) {
  const [serverUrl, setServerUrl] = useState('')
  const [googleOAuthConfigured, setGoogleOAuthConfigured] = useState<boolean | null>(null)
  const [microsoftOAuthConfigured, setMicrosoftOAuthConfigured] = useState<boolean | null>(null)
  const [theme, setTheme] = useState<Theme>((localStorage.getItem('muesli-theme') as Theme) || 'system')
  const [autoRecordDetectedMeetings, setAutoRecordDetectedMeetings] = useState<boolean>(
    () => loadCalendarPrefs().autoRecordDetectedMeetings,
  )
  const [digestCadence, setDigestCadence] = useState<DigestConfig['cadence']>('off')
  const navigate = useNavigate()

  useEffect(() => {
    muesli.getConfig().then((cfg) => setServerUrl(cfg?.serverUrl ?? ''))
  }, [])

  useEffect(() => {
    if (!serverUrl) {
      setGoogleOAuthConfigured(false)
      return
    }

    let cancelled = false
    void muesli.getGoogleCalendarOAuthStatus()
      .then((status) => {
        if (!cancelled) setGoogleOAuthConfigured(status.configured)
      })
      .catch(() => {
        if (!cancelled) setGoogleOAuthConfigured(false)
      })

    return () => {
      cancelled = true
    }
  }, [serverUrl])

  useEffect(() => {
    if (!serverUrl) {
      setMicrosoftOAuthConfigured(false)
      return
    }

    let cancelled = false
    void muesli.getMicrosoftCalendarOAuthStatus()
      .then((status) => {
        if (!cancelled) setMicrosoftOAuthConfigured(status.configured)
      })
      .catch(() => {
        if (!cancelled) setMicrosoftOAuthConfigured(false)
      })

    return () => {
      cancelled = true
    }
  }, [serverUrl])

  useEffect(() => {
    let cancelled = false
    void muesli.getDigestConfig()
      .then((cfg) => {
        if (!cancelled) setDigestCadence(cfg.cadence)
      })
      .catch(() => {
        if (!cancelled) setDigestCadence('off')
      })

    return () => {
      cancelled = true
    }
  }, [])

  function applyTheme(t: Theme) {
    setTheme(t)
    localStorage.setItem('muesli-theme', t)
    const dark = t === 'dark' || (t === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
    document.documentElement.classList.toggle('dark', dark)
  }

  return (
    <div className="mx-auto max-w-xl p-8">
      <h1 className="mb-6 text-xl font-semibold">Settings</h1>
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">Server</h2>
        <p className="text-sm">{serverUrl || 'Not connected'}</p>
        <ServerHealthBadge serverUrl={serverUrl} />
      </section>
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">Appearance</h2>
        <div className="flex gap-2">
          {(['system', 'light', 'dark'] as Theme[]).map((t) => (
            <Button key={t} variant={theme === t ? 'primary' : 'secondary'} size="sm" onClick={() => applyTheme(t)}>
              {t}
            </Button>
          ))}
        </div>
      </section>
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">Templates</h2>
        <Button variant="secondary" onClick={() => navigate('/templates')}>
          Manage templates
        </Button>
      </section>
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">Calendar</h2>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={autoRecordDetectedMeetings}
            onChange={(e) => {
              const next = e.target.checked
              setAutoRecordDetectedMeetings(next)
              saveCalendarPrefs({ autoRecordDetectedMeetings: next })
            }}
          />
          <span>Auto-record detected meetings</span>
        </label>
        {(googleOAuthConfigured || microsoftOAuthConfigured) ? (
          <div className="mt-3 flex flex-wrap gap-2">
            {googleOAuthConfigured ? (
              <Button
                variant="secondary"
                onClick={async () => {
                  try {
                    await muesli.openGoogleCalendarOAuthStart()
                  } catch (err) {
                    console.error('failed to open Google Calendar OAuth start URL', err)
                  }
                }}
              >
                Connect Google Calendar
              </Button>
            ) : null}
            {microsoftOAuthConfigured ? (
              <Button
                variant="secondary"
                onClick={async () => {
                  try {
                    await muesli.openMicrosoftCalendarOAuthStart()
                  } catch (err) {
                    console.error('failed to open Microsoft Calendar OAuth start URL', err)
                  }
                }}
              >
                Connect Microsoft Calendar
              </Button>
            ) : null}
          </div>
        ) : null}
      </section>
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">Digest</h2>
        <label className="flex items-center gap-2 text-sm">
          <span className="min-w-24">Cadence</span>
          <select
            className="rounded border px-2 py-1"
            value={digestCadence}
            onChange={async (e) => {
              const next = e.target.value as DigestConfig['cadence']
              const prev = digestCadence
              setDigestCadence(next)
              try {
                await muesli.updateDigestConfig(next)
              } catch (err) {
                console.error('failed to update digest cadence', err)
                setDigestCadence(prev)
              }
            }}
          >
            <option value="off">off</option>
            <option value="daily">daily</option>
            <option value="weekly">weekly</option>
          </select>
        </label>
      </section>
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">Tags</h2>
        <Button variant="secondary" onClick={() => navigate('/settings/tags')}>
          Manage tags
        </Button>
      </section>
      <Button
        variant="destructive"
        onClick={async () => {
          await muesli.disconnect()
          onDisconnected()
        }}
      >
        Disconnect
      </Button>
    </div>
  )
}

export function ServerHealthBadge({ serverUrl }: { serverUrl: string }) {
  const [health, setHealth] = useState<HealthState>({ status: 'idle' })

  useEffect(() => {
    if (!serverUrl) {
      setHealth({ status: 'idle' })
      return
    }

    const controller = new AbortController()
    let cancelled = false
    setHealth({ status: 'checking' })

    async function checkHealth() {
      try {
        const res = await fetch(healthUrlFor(serverUrl), { signal: controller.signal })
        if (!res.ok) throw new Error('health check failed')
        const data: unknown = await res.json()
        if (!data || typeof data !== 'object' || (data as Record<string, unknown>).status !== 'ok') {
          throw new Error('invalid health response')
        }
        if (!cancelled) setHealth({ status: 'connected', version: versionFromHealth(data as Record<string, unknown>) })
      } catch (err) {
        if (!cancelled && !(err instanceof DOMException && err.name === 'AbortError')) {
          setHealth({ status: 'unreachable' })
        }
      }
    }

    void checkHealth()

    return () => {
      cancelled = true
      controller.abort()
    }
  }, [serverUrl])

  if (health.status === 'idle') return null

  return (
    <span
      role="status"
      aria-live="polite"
      className={`mt-2 inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${
        health.status === 'unreachable'
          ? 'bg-destructive/10 text-destructive'
          : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300'
      }`}
    >
      {health.status === 'connected'
        ? health.version
          ? `Connected · v${health.version}`
          : 'Connected'
        : health.status === 'unreachable'
          ? 'Unreachable'
          : 'Checking'}
    </span>
  )
}
