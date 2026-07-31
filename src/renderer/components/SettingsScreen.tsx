import { useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { Checkbox } from '@/components/ui/Checkbox'
import { Dialog } from '@/components/ui/Dialog'
import { Select } from '@/components/ui/Select'
import { loadCalendarPrefs, saveCalendarPrefs } from '@/lib/calendarPrefs'
import { useToast } from '@/components/ui/Toast'
import type { DigestConfig } from '../../shared/types'

type Theme = 'system' | 'light' | 'dark'
type HealthState =
  | { status: 'idle' }
  | { status: 'checking' }
  | { status: 'connected'; version?: string }
  | { status: 'sign-in-required' }
  | { status: 'unreachable' }

const AI_SECTION_ID = 'ai-transcription'
const CALENDAR_SECTION_ID = 'calendar'

function AiTranscriptionSection() {
  const [ollamaDetected, setOllamaDetected] = useState<boolean | null>(null)
  const sectionRef = useRef<HTMLElement | null>(null)
  const location = useLocation()

  useEffect(() => {
    if (location.hash !== `#${AI_SECTION_ID}`) return
    if (typeof sectionRef.current?.scrollIntoView === 'function') {
      sectionRef.current.scrollIntoView({ block: 'start', behavior: 'smooth' })
    }
    sectionRef.current?.focus()
  }, [location.hash])

  useEffect(() => {
    let cancelled = false
    let intervalId: number | null = null

    const refresh = async () => {
      try {
        const status = await muesli.getReadyz()
        if (!cancelled) setOllamaDetected(status?.ollamaDetected ?? null)
      } catch {
        if (!cancelled) setOllamaDetected(null)
      }
    }

    void refresh()
    intervalId = window.setInterval(() => {
      void refresh()
    }, 1000)

    return () => {
      cancelled = true
      if (intervalId !== null) window.clearInterval(intervalId)
    }
  }, [])

  const detected = ollamaDetected === true
  const detectedLabel = ollamaDetected === null ? 'Detection unavailable' : detected ? 'Detected' : 'Not detected'

  return (
    <section
      id={AI_SECTION_ID}
      ref={sectionRef}
      tabIndex={-1}
      className="mb-6 scroll-mt-6"
      aria-labelledby="ai-transcription-heading"
    >
      <h2 id="ai-transcription-heading" className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">
        AI / Transcription
      </h2>
      <p className="text-sm">
        Ollama status: <span className="font-medium text-foreground">{detectedLabel}</span>
      </p>
      <p className="mt-1 text-sm text-muted-foreground">
        {detected
          ? 'Summaries and search are available.'
          : 'Without Ollama, summaries and search stay unavailable.'}
      </p>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <a
          className="inline-flex h-10 items-center justify-center rounded-[var(--radius)] bg-muted px-4 text-sm font-medium text-foreground hover:bg-muted/80"
          href="https://ollama.com/download"
          target="_blank"
          rel="noreferrer"
        >
          Download Ollama
        </a>
        <span className="text-xs text-muted-foreground">
          Detection updates live while this screen is open.
        </span>
      </div>
    </section>
  )
}

function CalendarSection() {
  const sectionRef = useRef<HTMLElement | null>(null)
  const location = useLocation()

  useEffect(() => {
    if (location.hash !== `#${CALENDAR_SECTION_ID}`) return
    if (typeof sectionRef.current?.scrollIntoView === 'function') {
      sectionRef.current.scrollIntoView({ block: 'start', behavior: 'smooth' })
    }
    sectionRef.current?.focus()
  }, [location.hash])

  return (
    <section
      id={CALENDAR_SECTION_ID}
      ref={sectionRef}
      tabIndex={-1}
      className="mb-6 scroll-mt-6"
      aria-labelledby="calendar-heading"
    >
      <h2 id="calendar-heading" className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">Calendar</h2>
      <p className="text-sm text-muted-foreground">
        Control how calendar events are connected and when detected meetings are auto-recorded.
      </p>
    </section>
  )
}

export function SettingsScreen({
  onDisconnected,
  onResetToBuiltIn,
}: {
  onDisconnected: () => void
  onResetToBuiltIn: () => void
}) {
  const [serverUrl, setServerUrl] = useState('')
  const [googleOAuthConfigured, setGoogleOAuthConfigured] = useState<boolean | null>(null)
  const [microsoftOAuthConfigured, setMicrosoftOAuthConfigured] = useState<boolean | null>(null)
  const [theme, setTheme] = useState<Theme>((localStorage.getItem('muesli-theme') as Theme) || 'system')
  const [autoRecordDetectedMeetings, setAutoRecordDetectedMeetings] = useState<boolean>(
    () => loadCalendarPrefs().autoRecordDetectedMeetings,
  )
  const [digestCadence, setDigestCadence] = useState<DigestConfig['cadence']>('off')
  const [confirmDisconnect, setConfirmDisconnect] = useState(false)
  const [confirmResetToBuiltIn, setConfirmResetToBuiltIn] = useState(false)
  const navigate = useNavigate()
  const { notify } = useToast()

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
      <AiTranscriptionSection />
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">Appearance</h2>
        <div className="flex gap-2">
          {(['system', 'light', 'dark'] as Theme[]).map((t) => (
            <Button
              key={t}
              variant={theme === t ? 'primary' : 'secondary'}
              size="sm"
              aria-pressed={theme === t}
              onClick={() => applyTheme(t)}
            >
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
      <CalendarSection />
      <section className="mb-6">
        <label htmlFor="auto-record-detected-meetings" className="flex items-center gap-2 text-sm">
          <Checkbox
            id="auto-record-detected-meetings"
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
                  notify(err instanceof Error ? err.message : 'Could not open Google Calendar OAuth start URL', 'error')
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
                  notify(err instanceof Error ? err.message : 'Could not open Microsoft Calendar OAuth start URL', 'error')
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
        <label htmlFor="digest-cadence" className="flex items-center gap-2 text-sm">
          <span className="min-w-24">Cadence</span>
          <Select
            id="digest-cadence"
            value={digestCadence}
            onChange={async (e) => {
              const next = e.target.value as DigestConfig['cadence']
              const prev = digestCadence
              setDigestCadence(next)
              try {
                await muesli.updateDigestConfig(next)
              } catch (err) {
                console.error('failed to update digest cadence', err)
                notify(err instanceof Error ? err.message : 'Could not update digest cadence', 'error')
                setDigestCadence(prev)
              }
            }}
          >
            <option value="off">off</option>
            <option value="daily">daily</option>
            <option value="weekly">weekly</option>
          </Select>
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
        onClick={() => setConfirmDisconnect(true)}
      >
        Disconnect
      </Button>
      <div className="mt-3">
        <Button
          variant="secondary"
          onClick={() => setConfirmResetToBuiltIn(true)}
        >
          Use this device&apos;s built-in server
        </Button>
      </div>
      {confirmDisconnect ? (
        <Dialog open onOpenChange={(o) => !o && setConfirmDisconnect(false)} title="Disconnect from this server?">
          <p className="text-sm text-muted-foreground">
            Disconnect from this server? You will need to sign in again to reconnect.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" size="sm" onClick={() => setConfirmDisconnect(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={async () => {
                try {
                  await muesli.disconnect()
                  setConfirmDisconnect(false)
                  onDisconnected()
                } catch (err) {
                  setConfirmDisconnect(false)
                  notify(err instanceof Error ? err.message : 'Could not disconnect from server', 'error')
                }
              }}
            >
              Disconnect
            </Button>
          </div>
        </Dialog>
      ) : null}
      {confirmResetToBuiltIn ? (
        <Dialog
          open
          onOpenChange={(o) => !o && setConfirmResetToBuiltIn(false)}
          title="Switch to this device's built-in server?"
        >
          <p className="text-sm text-muted-foreground">
            Switch to this device&apos;s built-in server? Your connection to the current server will be forgotten.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" size="sm" onClick={() => setConfirmResetToBuiltIn(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={async () => {
                try {
                  await muesli.resetToBuiltIn()
                  const cfg = await muesli.getConfig()
                  setServerUrl(cfg?.serverUrl ?? '')
                  setConfirmResetToBuiltIn(false)
                  await onResetToBuiltIn()
                } catch (err) {
                  setConfirmResetToBuiltIn(false)
                  notify(err instanceof Error ? err.message : 'Could not switch to built-in server', 'error')
                }
              }}
            >
              Switch to built-in server
            </Button>
          </div>
        </Dialog>
      ) : null}
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

    let cancelled = false
    setHealth({ status: 'checking' })

    async function checkHealth() {
      try {
        const result = await muesli.getServerHealth()
        if (cancelled) return
        if (!result.reachable) {
          setHealth({ status: 'unreachable' })
          return
        }
        if (!result.authenticated) {
          setHealth({ status: 'sign-in-required' })
          return
        }
        setHealth({ status: 'connected', version: result.version })
      } catch {
        if (!cancelled) setHealth({ status: 'unreachable' })
      }
    }

    void checkHealth()

    return () => {
      cancelled = true
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
          : health.status === 'sign-in-required'
            ? 'bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200'
          : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300'
      }`}
    >
      {health.status === 'connected'
        ? health.version
          ? `Connected · v${health.version}`
          : 'Connected'
        : health.status === 'sign-in-required'
          ? 'Sign in required'
        : health.status === 'unreachable'
          ? 'Unreachable'
          : 'Checking'}
    </span>
  )
}
