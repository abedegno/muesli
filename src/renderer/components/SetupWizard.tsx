import { ServiceReadiness } from './ServiceReadiness'
import { LanguageSettings } from './LanguageSettings'
import { uiText } from '@/i18n'
import { useEffect, useRef, useState } from 'react'
import { ArrowLeft, Check, FileText, Mic, ShieldCheck, Sparkles } from 'lucide-react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { LocalAiSetup, type LocalAiSetupBridge } from './LocalAiSetup'
import { TranscriptionPreferences, type TranscriptionBridge } from './TranscriptionPreferences'
import { t } from '@/i18n'
import { ComputerAudioSetup, type ComputerAudioBridge } from './ComputerAudioSetup'
import type { MicStatus } from '../../main/micPermission'
import type { MuesliBridge } from '../../shared/ipc'

export type SetupWizardStep = 'welcome' | 'microphone' | 'transcription' | 'ai' | 'done'

export function nextStep(step: SetupWizardStep): SetupWizardStep {
  switch (step) {
    case 'welcome':
      return 'microphone'
    case 'microphone':
      return 'transcription'
    case 'transcription':
      return 'ai'
    case 'ai':
      return 'done'
    case 'done':
      return 'done'
  }
}

export type SetupWizardBridge = ComputerAudioBridge &
  LocalAiSetupBridge &
  TranscriptionBridge &
  Pick<
    MuesliBridge,
    'platform' | 'micStatus' | 'micRequest' | 'micOpenSettings' | 'getReadyz' | 'getManualServer'
  > &
  Partial<Pick<MuesliBridge, 'getServiceReadiness' | 'retryAIModels'>>

export function SetupWizard({
  onDone,
  bridge = muesli,
}: {
  onDone: () => void
  bridge?: SetupWizardBridge
}) {
  const [step, setStep] = useState<SetupWizardStep>('welcome')
  const [micStatus, setMicStatus] = useState<MicStatus | null>(null)
  const [readyz, setReadyz] = useState<{ ollamaDetected: boolean } | null>(null)
  const [mode, setMode] = useState<'checking' | 'local' | 'hosted' | 'error'>('checking')
  const [modeRetry, setModeRetry] = useState(0)
  const [aiRetry, setAiRetry] = useState(0)
  const [checkingAi, setCheckingAi] = useState(true)
  const [micError, setMicError] = useState<string | null>(null)
  const [micBusy, setMicBusy] = useState(false)
  const [transcriptionSaving, setTranscriptionSaving] = useState(false)
  const micBusyRef = useRef(false)
  const headingRef = useRef<HTMLHeadingElement>(null)
  const doneRef = useRef(false)

  useEffect(() => {
    headingRef.current?.focus()
  }, [step])

  useEffect(() => {
    let active = true
    setMode('checking')
    void bridge.getManualServer().then(
      (hosted) => {
        if (active) setMode(hosted ? 'hosted' : 'local')
      },
      () => {
        if (active) setMode('error')
      }
    )
    return () => {
      active = false
    }
  }, [bridge, modeRetry])

  useEffect(() => {
    let active = true
    if (step === 'microphone') {
      void (async () => {
        try {
          const status = await bridge.micStatus()
          if (active) setMicStatus(status)
        } catch {
          if (active) setMicStatus(null)
        }
      })()
    }
    return () => {
      active = false
    }
  }, [bridge, step])

  useEffect(() => {
    let active = true
    if (step === 'ai') {
      setCheckingAi(true)
      setReadyz(null)
      void (async () => {
        try {
          const status = await bridge.getReadyz()
          if (active) setReadyz(status)
        } catch {
          if (active) setReadyz(null)
        } finally {
          if (active) setCheckingAi(false)
        }
      })()
    }
    return () => {
      active = false
    }
  }, [bridge, step, aiRetry])

  useEffect(() => {
    if (step !== 'done' || doneRef.current) return
    doneRef.current = true
    onDone()
  }, [onDone, step])

  const canContinue = step !== 'done' && !transcriptionSaving

  async function refreshMic() {
    try {
      const status = await bridge.micStatus()
      setMicStatus(status)
    } catch {
      setMicStatus(null)
    }
  }

  async function grantMic() {
    if (micBusyRef.current) return
    micBusyRef.current = true
    setMicBusy(true)
    setMicError(null)
    try {
      await bridge.micRequest()
    } catch {
      setMicError(
        'Could not request microphone access. Try again, or continue and enable it later in Settings.'
      )
    } finally {
      await refreshMic()
      micBusyRef.current = false
      setMicBusy(false)
    }
  }

  async function openMicSettings() {
    setMicError(null)
    try {
      await bridge.micOpenSettings()
    } catch {
      setMicError(
        'Could not open system settings. Open your device’s privacy settings and allow microphone access for Muesli.'
      )
    }
  }

  function goNext() {
    setStep((current) => nextStep(current))
  }

  return (
    <main className="flex h-full min-h-0 items-center justify-center bg-app-chrome px-4 py-4 text-foreground sm:px-6 sm:py-6">
      <section
        aria-labelledby="setup-heading"
        className="flex max-h-full min-h-0 w-full max-w-2xl flex-col overflow-hidden rounded-2xl border border-border bg-card shadow-md"
      >
        <header className="shrink-0 border-b border-border px-6 py-4 sm:px-10 sm:py-6">
          <div className="flex items-center gap-3">
            <span className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary/10 text-primary">
              <Mic aria-hidden="true" className="h-5 w-5" />
            </span>
            <div>
              <p className="font-serif text-2xl">{uiText('Muesli')}</p>
              <p className="text-xs text-muted-foreground">
                {uiText('A little setup. Better meeting notes.')}
              </p>
            </div>
          </div>
          <ol
            aria-label={uiText('Setup progress')}
            className="mt-6 flex gap-3 text-xs sm:gap-6 sm:text-sm"
          >
            {(['welcome', 'microphone', 'transcription', 'ai'] as const).map((item, index) => {
              const current = item === step
              const complete =
                ['welcome', 'microphone', 'transcription', 'ai', 'done'].indexOf(step) > index
              return (
                <li
                  key={item}
                  aria-current={current ? 'step' : undefined}
                  className={`flex items-center gap-2 ${current ? 'font-medium text-foreground' : 'text-muted-foreground'}`}
                >
                  <span
                    className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full ${current || complete ? 'bg-primary text-primary-foreground' : 'bg-muted'}`}
                  >
                    {complete ? <Check aria-hidden="true" className="h-3 w-3" /> : index + 1}
                  </span>
                  <span className={current ? '' : 'sr-only sm:not-sr-only'}>
                    {item === 'welcome'
                      ? uiText('Welcome')
                      : item === 'microphone'
                        ? uiText('Microphone')
                        : item === 'transcription'
                          ? t('transcription.title')
                          : uiText('AI features')}
                  </span>
                </li>
              )
            })}
          </ol>
        </header>
        <div
          role="region"
          aria-label={uiText('Setup step')}
          // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- Keyboard access to this scroll region or separator.
          tabIndex={0}
          className="min-h-0 overflow-y-auto"
        >
          <div className="px-6 py-6 sm:px-10 sm:py-8">
            {step === 'welcome' && (
              <div className="space-y-5">
                <LanguageSettings compact />
                <h1
                  id="setup-heading"
                  ref={headingRef}
                  tabIndex={-1}
                  className="font-serif text-3xl tracking-tight outline-none"
                >
                  {uiText('Let us set up the basics')}
                </h1>
                <p className="text-base leading-7 text-foreground">
                  {uiText(
                    'Capture the conversation, keep your own notes, and come back to a clear record of your meeting.'
                  )}
                </p>
                <div className="grid gap-4 sm:grid-cols-2">
                  <div className="rounded-xl bg-muted/60 p-4">
                    <FileText aria-hidden="true" className="mb-3 h-5 w-5 text-primary" />
                    <p className="text-sm font-medium">{uiText('Stay in the conversation')}</p>
                    <p className="mt-1 text-sm leading-6 text-muted-foreground">
                      {uiText('Record and transcribe while you take notes at your own pace.')}
                    </p>
                  </div>
                  <div className="rounded-xl bg-muted/60 p-4">
                    <Sparkles aria-hidden="true" className="mb-3 h-5 w-5 text-primary" />
                    <p className="text-sm font-medium">{uiText('Make sense of it later')}</p>
                    <p className="mt-1 text-sm leading-6 text-muted-foreground">
                      {uiText('Add optional AI summaries and search when you’re ready.')}
                    </p>
                  </div>
                </div>
                <div className="flex items-start gap-3 rounded-xl border border-border p-4 text-sm leading-6">
                  <ShieldCheck
                    aria-hidden="true"
                    className="mt-0.5 h-5 w-5 shrink-0 text-primary"
                  />
                  <p>
                    {mode === 'hosted'
                      ? uiText(
                          'Your recordings and notes are processed by your connected server. Your administrator manages its storage and AI services.'
                        )
                      : mode === 'local'
                        ? uiText(
                            'Your built-in server stores recordings and notes on this device. Recording and transcription work without a cloud account.'
                          )
                        : uiText(
                            'Muesli works with a built-in local server or a server managed by your team. We’ll check your connection as you get set up.'
                          )}
                  </p>
                </div>
              </div>
            )}

            {step === 'microphone' && (
              <div className="space-y-5">
                <h1
                  id="setup-heading"
                  ref={headingRef}
                  tabIndex={-1}
                  className="font-serif text-3xl tracking-tight outline-none"
                >
                  {uiText('Microphone access')}
                </h1>
                <p className="text-sm leading-6 text-muted-foreground">
                  {uiText('Muesli needs microphone access to record your meetings.')}
                </p>
                {micStatus === 'granted' ? (
                  <div
                    role="status"
                    className="flex items-center gap-2 rounded-xl border border-primary/20 bg-primary/10 px-4 py-3 text-sm text-foreground"
                  >
                    <Check aria-hidden="true" className="h-4 w-4 text-primary" />
                    <span>{uiText('Microphone access granted.')}</span>
                  </div>
                ) : micStatus === 'denied' || micStatus === 'restricted' ? (
                  <div className="space-y-3">
                    <p className="text-sm text-foreground">
                      {uiText(
                        'Microphone access is blocked. You can enable it in system settings and continue later.'
                      )}
                    </p>
                    <div className="flex flex-wrap gap-3">
                      <Button
                        type="button"
                        variant="secondary"
                        onClick={() => void openMicSettings()}
                      >
                        {uiText('Open System Settings')}
                      </Button>
                      <Button type="button" variant="ghost" onClick={() => void refreshMic()}>
                        {uiText('Re-check')}
                      </Button>
                    </div>
                  </div>
                ) : (
                  <div className="space-y-3">
                    <p className="text-sm text-muted-foreground">
                      {uiText('Grant access when prompted so Muesli can use your microphone.')}
                    </p>
                    <Button
                      type="button"
                      variant="secondary"
                      disabled={micBusy}
                      onClick={() => void grantMic()}
                    >
                      {micBusy ? uiText('Requesting access…') : uiText('Grant microphone')}
                    </Button>
                  </div>
                )}
                <ComputerAudioSetup bridge={bridge} />
                {micError && (
                  <p role="alert" className="text-sm text-destructive">
                    {micError}
                  </p>
                )}
              </div>
            )}

            {step === 'transcription' && (
              <div className="space-y-5">
                <h1
                  id="setup-heading"
                  ref={headingRef}
                  tabIndex={-1}
                  className="font-serif text-3xl tracking-tight outline-none"
                >
                  {t('transcription.title')}
                </h1>
                <TranscriptionPreferences bridge={bridge} onBusyChange={setTranscriptionSaving} />
              </div>
            )}

            {step === 'ai' && (
              <div className="space-y-5">
                <h1
                  id="setup-heading"
                  ref={headingRef}
                  tabIndex={-1}
                  className="font-serif text-3xl tracking-tight outline-none"
                >
                  {uiText('Optional AI features')}
                </h1>
                {!(mode === 'local' && !checkingAi && readyz?.ollamaDetected === false) && (
                  <p role="status" className="text-sm leading-6 text-muted-foreground">
                    {mode === 'checking'
                      ? uiText('Checking your connection…')
                      : mode === 'error'
                        ? uiText(
                            'Could not check your connection. Re-check to see the right setup instructions.'
                          )
                        : mode === 'hosted'
                          ? uiText(
                              'This server is managed remotely. Ask your administrator to configure the default AI agent for summaries and semantic search.'
                            )
                          : checkingAi
                            ? uiText('Checking AI availability…')
                            : readyz === null
                              ? uiText(
                                  'Could not check AI availability. Re-check, or continue and set up AI later in Settings.'
                                )
                              : readyz.ollamaDetected
                                ? uiText(
                                    'Ollama is detected. You can configure models for AI summaries & semantic search in Settings.'
                                  )
                                : uiText(
                                    'Optional: install Ollama to enable AI summaries & semantic search.'
                                  )}
                  </p>
                )}
                {mode === 'local' && !checkingAi && readyz?.ollamaDetected === false ? (
                  <LocalAiSetup bridge={bridge} />
                ) : null}
                {!(mode === 'local' && !checkingAi && readyz?.ollamaDetected === false) && (
                  <Button
                    type="button"
                    variant="secondary"
                    disabled={mode === 'checking' || checkingAi}
                    onClick={() => {
                      setAiRetry((n) => n + 1)
                      if (mode === 'error') setModeRetry((n) => n + 1)
                    }}
                  >
                    {uiText('Re-check')}
                  </Button>
                )}
                <ServiceReadiness bridge={bridge} />
                <p className="rounded-xl bg-muted/60 p-4 text-sm leading-6 text-muted-foreground">
                  {uiText(
                    'You can take notes, record, and transcribe without AI. Change your AI setup at any time in Settings.'
                  )}
                </p>
              </div>
            )}
          </div>
        </div>
        {step === 'done' ? null : (
          <div className="mx-6 flex shrink-0 flex-wrap items-center justify-between gap-4 border-t border-border py-5 sm:mx-10">
            <div className="text-sm text-muted-foreground">
              {step === 'ai'
                ? uiText('This step is optional.')
                : step === 'microphone'
                  ? uiText('You can enable this later.')
                  : step === 'transcription'
                    ? t('wizard.transcriptionHint')
                    : uiText('Continue to the next step.')}
            </div>
            <div className="flex items-center gap-2">
              {step !== 'welcome' && (
                <Button
                  type="button"
                  variant="ghost"
                  disabled={transcriptionSaving}
                  onClick={() =>
                    setStep(
                      step === 'ai'
                        ? 'transcription'
                        : step === 'transcription'
                          ? 'microphone'
                          : 'welcome'
                    )
                  }
                >
                  <ArrowLeft aria-hidden="true" className="h-4 w-4" />
                  {uiText('Back')}
                </Button>
              )}
              {step === 'ai' ? (
                <Button type="button" variant="ghost" onClick={goNext}>
                  {uiText('Skip')}
                </Button>
              ) : null}
              <Button type="button" disabled={!canContinue} onClick={goNext}>
                {uiText('Continue')}
              </Button>
            </div>
          </div>
        )}
      </section>
    </main>
  )
}
