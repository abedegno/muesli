import { LanguageSettings } from './LanguageSettings'
import { uiText } from '@/i18n'
import { CalendarReadiness } from './CalendarReadiness'
import { ServiceReadiness } from './ServiceReadiness'
import { useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { Checkbox } from '@/components/ui/Checkbox'
import { Dialog } from '@/components/ui/Dialog'
import { Select } from '@/components/ui/Select'
import { loadCalendarPrefs, saveCalendarPrefs } from '@/lib/calendarPrefs'
import { useToast } from '@/components/ui/Toast'
import { HelpTip } from '@/components/ui/HelpTip'
import { TranscriptionPreferences } from './TranscriptionPreferences'
import { LocalAiSetup } from './LocalAiSetup'
import { t } from '@/i18n'
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

function TranscriptionSection() {
  return (
    <section className="mb-8" aria-labelledby="transcription-heading">
      <h2
        id="transcription-heading"
        className="mb-3 text-sm font-medium uppercase tracking-wide text-muted-foreground"
      >
        {t('transcription.title')}
      </h2>
      <TranscriptionPreferences bridge={muesli} />
    </section>
  )
}

function AiTranscriptionSection({ hosted }: { hosted: boolean | null }) {
  const [detected, setDetected] = useState<boolean | null>(null)
  const [retry, setRetry] = useState(0)
  const sectionRef = useRef<HTMLElement | null>(null)
  const location = useLocation()
  useEffect(() => {
    if (location.hash === `#${AI_SECTION_ID}`) {
      sectionRef.current?.scrollIntoView?.({ block: 'start', behavior: 'smooth' })
      sectionRef.current?.focus()
    }
  }, [location.hash])
  useEffect(() => {
    let active = true
    if (hosted === false)
      void muesli
        .getReadyz()
        .then((status) => {
          if (active) setDetected(status?.ollamaDetected ?? null)
        })
        .catch(() => {
          if (active) setDetected(null)
        })
    return () => {
      active = false
    }
  }, [hosted, retry])
  return (
    <section
      id={AI_SECTION_ID}
      ref={sectionRef}
      tabIndex={-1}
      className="mb-8 scroll-mt-6"
      aria-labelledby="ai-transcription-heading"
    >
      <div className="flex items-center gap-1">
        <h2
          id="ai-transcription-heading"
          className="text-sm font-medium uppercase tracking-wide text-muted-foreground"
        >
          {t('settings.ai')}
        </h2>
        <HelpTip label={t('settings.ai')}>{t('settings.aiHelp')}</HelpTip>
      </div>
      <p className="my-3 text-sm leading-6 text-muted-foreground">{t('settings.aiHelp')}</p>
      <ServiceReadiness key={String(hosted)} />
      {hosted === null ? (
        <p>{t('common.loading')}</p>
      ) : hosted ? (
        <p className="text-sm leading-6">{t('settings.aiHosted')}</p>
      ) : detected === true ? (
        <p className="text-sm leading-6">{t('settings.aiDetected')}</p>
      ) : detected === false ? (
        <LocalAiSetup bridge={muesli} />
      ) : (
        <div className="space-y-2">
          <p className="text-sm text-muted-foreground">{t('settings.aiUnknown')}</p>
          <Button variant="secondary" onClick={() => setRetry((value) => value + 1)}>
            {t('common.retry')}
          </Button>
        </div>
      )}
    </section>
  )
}

function CalendarSection({ hosted }: { hosted: boolean | null }) {
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
      <h2
        id="calendar-heading"
        className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground"
      >
        {uiText('Calendar')}
      </h2>
      <p className="text-sm text-muted-foreground">{t('settings.calendarHelp')}</p>
      <CalendarReadiness hosted={hosted} />
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
  const [canAdminister, setCanAdminister] = useState(false)
  const [manualServer, setManualServer] = useState<boolean | null>(null)
  const [googleOAuthConfigured, setGoogleOAuthConfigured] = useState<boolean | null>(null)
  const [microsoftOAuthConfigured, setMicrosoftOAuthConfigured] = useState<boolean | null>(null)
  const [theme, setTheme] = useState<Theme>(
    (localStorage.getItem('muesli-theme') as Theme) || 'system'
  )
  const [keepRunningInBackground, setKeepRunningInBackground] = useState(false)
  const [autoRecordDetectedMeetings, setAutoRecordDetectedMeetings] = useState<boolean>(
    () => loadCalendarPrefs().autoRecordDetectedMeetings
  )
  const [digestCadence, setDigestCadence] = useState<DigestConfig['cadence']>('off')
  const [confirmDisconnect, setConfirmDisconnect] = useState(false)
  const [confirmResetToBuiltIn, setConfirmResetToBuiltIn] = useState(false)
  const navigate = useNavigate()
  const { notify } = useToast()

  useEffect(() => {
    let active = true
    void Promise.resolve(muesli.getCapabilities?.())
      .then((caps) => {
        if (active) setCanAdminister(caps?.isAdmin === true)
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [])

  useEffect(() => {
    void muesli
      .getConfig()
      .then((cfg) => {
        setServerUrl(cfg?.serverUrl ?? '')
        setManualServer(cfg?.manualServer ?? false)
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    if (!serverUrl) {
      setGoogleOAuthConfigured(false)
      return
    }

    let cancelled = false
    void muesli
      .getGoogleCalendarOAuthStatus()
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
    void muesli
      .getMicrosoftCalendarOAuthStatus()
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
    void muesli
      .getDigestConfig()
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

  useEffect(() => {
    let cancelled = false
    void muesli
      .getKeepRunningInBackground()
      .then((enabled) => {
        if (!cancelled) setKeepRunningInBackground(enabled)
      })
      .catch(() => {
        if (!cancelled) setKeepRunningInBackground(false)
      })

    return () => {
      cancelled = true
    }
  }, [])

  function applyTheme(t: Theme) {
    setTheme(t)
    localStorage.setItem('muesli-theme', t)
    const dark =
      t === 'dark' || (t === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
    document.documentElement.classList.toggle('dark', dark)
  }

  return (
    <div className="mx-auto max-w-xl p-8">
      <h1 className="mb-6 text-xl font-semibold">{uiText('Settings')}</h1>
      <p className="mb-6 text-sm leading-6 text-muted-foreground">{t('settings.intro')}</p>
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">
          {uiText('Appearance')}
        </h2>
        <div className="flex gap-2">
          {(['system', 'light', 'dark'] as Theme[]).map((mode) => (
            <Button
              key={mode}
              variant={theme === mode ? 'primary' : 'secondary'}
              size="sm"
              aria-pressed={theme === mode}
              onClick={() => applyTheme(mode)}
            >
              {t(
                mode === 'system'
                  ? 'settings.system'
                  : mode === 'light'
                    ? 'settings.light'
                    : 'settings.dark'
              )}
            </Button>
          ))}
        </div>
      </section>

      <LanguageSettings />
      <AiTranscriptionSection hosted={manualServer} />
      <TranscriptionSection />
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">
          {uiText('Templates')}
        </h2>
        <p className="mb-3 text-sm leading-6 text-muted-foreground">
          {t('settings.templatesHelp')}
        </p>
        <Button variant="secondary" onClick={() => navigate('/templates')}>
          {uiText('Manage templates')}
        </Button>
      </section>
      <CalendarSection hosted={manualServer} />
      <section className="mb-6">
        <label htmlFor="keep-running-in-background" className="flex items-center gap-2 text-sm">
          <Checkbox
            id="keep-running-in-background"
            checked={keepRunningInBackground}
            onChange={async (e) => {
              const next = e.target.checked
              const prev = keepRunningInBackground
              setKeepRunningInBackground(next)
              try {
                await muesli.setKeepRunningInBackground(next)
              } catch (err) {
                console.error('failed to update keep-running preference', err)
                notify(
                  err instanceof Error
                    ? err.message
                    : uiText('Could not update menu bar preference'),
                  'error'
                )
                setKeepRunningInBackground(prev)
              }
            }}
          />
          <span>{uiText('Keep Muesli running in the menu bar when the window is closed')}</span>
        </label>
        <p className="mt-1 text-xs text-muted-foreground">{t('settings.backgroundHelp')}</p>
      </section>
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
          <span>{uiText('Auto-record detected meetings')}</span>
        </label>
        <p className="mt-1 text-xs text-muted-foreground">{t('settings.recordHelp')}</p>
        {googleOAuthConfigured || microsoftOAuthConfigured ? (
          <div className="mt-3 flex flex-wrap gap-2">
            {googleOAuthConfigured ? (
              <Button
                variant="secondary"
                onClick={async () => {
                  try {
                    await muesli.openGoogleCalendarOAuthStart()
                  } catch (err) {
                    console.error('failed to open Google Calendar OAuth start URL', err)
                    notify(
                      err instanceof Error
                        ? err.message
                        : uiText('Could not open Google Calendar OAuth start URL'),
                      'error'
                    )
                  }
                }}
              >
                {uiText('Connect Google Calendar')}
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
                    notify(
                      err instanceof Error
                        ? err.message
                        : uiText('Could not open Microsoft Calendar OAuth start URL'),
                      'error'
                    )
                  }
                }}
              >
                {uiText('Connect Microsoft Calendar')}
              </Button>
            ) : null}
          </div>
        ) : null}
      </section>
      <details className="mb-8 rounded-xl border border-border p-4">
        <summary className="cursor-pointer text-sm font-medium">
          {t('settings.integrations')}
        </summary>
        <p className="my-3 text-sm leading-6 text-muted-foreground">{t('settings.digestHelp')}</p>
        <h2 className="mb-2 text-sm font-medium text-muted-foreground">{t('settings.digest')}</h2>
        <label htmlFor="digest-cadence" className="flex items-center gap-2 text-sm">
          <span className="min-w-24">{t('settings.digestFrequency')}</span>
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
                notify(
                  err instanceof Error ? err.message : uiText('Could not update digest cadence'),
                  'error'
                )
                setDigestCadence(prev)
              }
            }}
          >
            <option value="off">{t('settings.digestOff')}</option>
            <option value="daily">{t('settings.digestDaily')}</option>
            <option value="weekly">{t('settings.digestWeekly')}</option>
          </Select>
        </label>
      </details>
      <section className="mb-6">
        <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">
          {uiText('Tags')}
        </h2>
        <p className="mb-3 text-sm text-muted-foreground">{t('settings.tagsHelp')}</p>
        <Button variant="secondary" onClick={() => navigate('/settings/tags')}>
          {uiText('Manage tags')}
        </Button>
      </section>
      <details className="mb-8 rounded-xl border border-border p-4">
        <summary className="cursor-pointer text-sm font-medium">{t('settings.connection')}</summary>
        <p className="my-3 text-sm leading-6 text-muted-foreground">
          {t('settings.connectionHelp')}
        </p>
        {manualServer !== null && (
          <p className="mb-3 text-sm">
            {t(manualServer ? 'settings.connectionHosted' : 'settings.connectionLocal')}
          </p>
        )}
        <section className="mb-6">
          <h2 className="mb-2 text-sm font-medium uppercase tracking-wide text-muted-foreground">
            {uiText('Server')}
          </h2>
          <p className="text-sm">{serverUrl || uiText('Not connected')}</p>
          <ServerHealthBadge serverUrl={serverUrl} />
          {manualServer && canAdminister && serverUrl && (
            <a
              className="mt-3 inline-block text-sm underline"
              href={`${serverUrl.replace(/\/+$/, '')}/admin/`}
              target="_blank"
              rel="noreferrer"
            >
              {uiText('Open server administration')}
            </a>
          )}
        </section>
        <Button variant="destructive" onClick={() => setConfirmDisconnect(true)}>
          {uiText('Disconnect')}
        </Button>
        <div className="mt-3">
          <Button variant="secondary" onClick={() => setConfirmResetToBuiltIn(true)}>
            {uiText("Use this device's built-in server")}
          </Button>
        </div>
      </details>
      {confirmDisconnect ? (
        <Dialog
          open
          onOpenChange={(o) => !o && setConfirmDisconnect(false)}
          title={uiText('Disconnect from this server?')}
        >
          <p className="text-sm text-muted-foreground">
            {uiText('Disconnect from this server? You will need to sign in again to reconnect.')}
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" size="sm" onClick={() => setConfirmDisconnect(false)}>
              {uiText('Cancel')}
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
                  notify(
                    err instanceof Error ? err.message : uiText('Could not disconnect from server'),
                    'error'
                  )
                }
              }}
            >
              {uiText('Disconnect')}
            </Button>
          </div>
        </Dialog>
      ) : null}
      {confirmResetToBuiltIn ? (
        <Dialog
          open
          onOpenChange={(o) => !o && setConfirmResetToBuiltIn(false)}
          title={uiText("Switch to this device's built-in server?")}
        >
          <p className="text-sm text-muted-foreground">
            {uiText(
              "Switch to this device's built-in server? Your connection to the current server will be forgotten."
            )}
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <Button variant="secondary" size="sm" onClick={() => setConfirmResetToBuiltIn(false)}>
              {uiText('Cancel')}
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={async () => {
                try {
                  await muesli.resetToBuiltIn()
                  const cfg = await muesli.getConfig()
                  setServerUrl(cfg?.serverUrl ?? '')
                  setManualServer(cfg?.manualServer ?? false)
                  setConfirmResetToBuiltIn(false)
                  await onResetToBuiltIn()
                } catch (err) {
                  setConfirmResetToBuiltIn(false)
                  notify(
                    err instanceof Error
                      ? err.message
                      : uiText('Could not switch to built-in server'),
                    'error'
                  )
                }
              }}
            >
              {uiText('Switch to built-in server')}
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
          ? uiText('Connected · v{value1}', { value1: health.version })
          : uiText('Connected')
        : health.status === 'sign-in-required'
          ? uiText('Sign in required')
          : health.status === 'unreachable'
            ? uiText('Unreachable')
            : uiText('Checking')}
    </span>
  )
}
