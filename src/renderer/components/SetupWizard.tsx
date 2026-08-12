import { useEffect, useRef, useState } from 'react'
import { Check } from 'lucide-react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import type { MicStatus } from '../../main/micPermission'
import type { MuesliBridge } from '../../shared/ipc'

export type SetupWizardStep = 'welcome' | 'microphone' | 'ai' | 'done'

export function nextStep(step: SetupWizardStep): SetupWizardStep {
  switch (step) {
    case 'welcome':
      return 'microphone'
    case 'microphone':
      return 'ai'
    case 'ai':
      return 'done'
    case 'done':
      return 'done'
  }
}

export type SetupWizardBridge = Pick<MuesliBridge, 'platform' | 'micStatus' | 'micRequest' | 'micOpenSettings' | 'getReadyz' | 'getManualServer'>

function ollamaInstallHint(platform: NodeJS.Platform): { label: string; command: string | null } {
  switch (platform) {
    case 'darwin':
      return { label: 'macOS (Homebrew):', command: 'brew install ollama' }
    case 'linux':
      return { label: 'Linux:', command: 'curl -fsSL https://ollama.com/install.sh | sh' }
    case 'win32':
      return { label: 'Windows:', command: null }
    default:
      return { label: 'Download for your platform:', command: null }
  }
}

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
  const [manualServer, setManualServer] = useState(false)
  const doneRef = useRef(false)

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
      void (async () => {
        try {
          const hosted = await bridge.getManualServer()
          if (active) setManualServer(hosted)
        } catch {
          if (active) setManualServer(false)
        }
        try {
          const status = await bridge.getReadyz()
          if (active) setReadyz(status)
        } catch {
          if (active) setReadyz(null)
        }
      })()
    }
    return () => {
      active = false
    }
  }, [bridge, step])

  useEffect(() => {
    if (step !== 'done' || doneRef.current) return
    doneRef.current = true
    onDone()
  }, [onDone, step])

  const canContinue = step === 'welcome' || step === 'microphone' || step === 'ai'

  async function refreshMic() {
    try {
      const status = await bridge.micStatus()
      setMicStatus(status)
    } catch {
      setMicStatus(null)
    }
  }

  async function refreshReadyz() {
    try {
      const status = await bridge.getReadyz()
      setReadyz(status)
    } catch {
      setReadyz(null)
    }
  }

  async function grantMic() {
    try {
      await bridge.micRequest()
    } finally {
      await refreshMic()
    }
  }

  function goNext() {
    setStep((current) => nextStep(current))
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-app-chrome px-6 py-10 text-foreground">
      <section className="w-full max-w-2xl rounded-[var(--radius)] border border-border bg-card px-8 py-7 shadow-md">
        <p className="text-xs font-semibold uppercase tracking-[0.25em] text-muted-foreground">Welcome to Muesli</p>
        <div className="mt-4">
          {step === 'welcome' && (
            <div className="space-y-5">
              <h1 className="text-2xl font-semibold tracking-tight">Let us set up the basics</h1>
              <p className="text-base leading-7 text-foreground">
                Muesli records and transcribes your meetings, all on your device.
              </p>
            </div>
          )}

          {step === 'microphone' && (
            <div className="space-y-5">
              <h1 className="text-2xl font-semibold tracking-tight">Microphone access</h1>
              <p className="text-sm leading-6 text-muted-foreground">
                Muesli needs microphone access to record your meetings.
              </p>
              {micStatus === 'granted' ? (
                <div className="flex items-center gap-2 rounded-[var(--radius)] border border-emerald-500/20 bg-emerald-500/10 px-3 py-2 text-sm text-emerald-700">
                  <Check className="h-4 w-4" />
                  <span>Microphone access granted.</span>
                </div>
              ) : micStatus === 'denied' || micStatus === 'restricted' ? (
                <div className="space-y-3">
                  <p className="text-sm text-amber-700">
                    Microphone access is blocked. You can enable it in system settings and continue later.
                  </p>
                  <div className="flex flex-wrap gap-3">
                    <Button type="button" variant="secondary" onClick={() => void bridge.micOpenSettings()}>
                      Open System Settings
                    </Button>
                    <Button type="button" variant="ghost" onClick={() => void refreshMic()}>
                      Re-check
                    </Button>
                  </div>
                </div>
              ) : (
                <div className="space-y-3">
                  <p className="text-sm text-muted-foreground">
                    Grant access when prompted so Muesli can use your microphone.
                  </p>
                  <Button type="button" variant="secondary" onClick={() => void grantMic()}>
                    Grant microphone
                  </Button>
                </div>
              )}
            </div>
          )}

          {step === 'ai' && (
            <div className="space-y-5">
              <h1 className="text-2xl font-semibold tracking-tight">Optional AI features</h1>
              <p className="text-sm leading-6 text-muted-foreground">
                {readyz?.ollamaDetected
                  ? 'AI summaries & semantic search are ready.'
                  : manualServer
                    ? 'This server is managed remotely. Ask your administrator to configure the default AI agent for summaries and semantic search.'
                    : 'Optional: install Ollama to enable AI summaries & semantic search.'}
              </p>
              {!readyz?.ollamaDetected && !manualServer ? (
                <div className="space-y-3">
                  <p className="text-sm text-muted-foreground">{ollamaInstallHint(bridge.platform).label}</p>
                  {ollamaInstallHint(bridge.platform).command ? (
                    <code className="block rounded bg-muted px-3 py-2 text-xs">
                      {ollamaInstallHint(bridge.platform).command}
                    </code>
                  ) : null}
                  <p className="text-sm text-muted-foreground">
                    <a
                      className="underline underline-offset-2 hover:no-underline"
                      href="https://ollama.com"
                      target="_blank"
                      rel="noreferrer"
                    >
                      Visit ollama.com
                    </a>
                  </p>
                  <Button type="button" variant="ghost" onClick={() => void refreshReadyz()}>
                    Re-check
                  </Button>
                </div>
              ) : !readyz?.ollamaDetected && manualServer ? (
                <div className="space-y-3">
                  <Button type="button" variant="ghost" onClick={() => void refreshReadyz()}>
                    Re-check
                  </Button>
                </div>
              ) : null}
            </div>
          )}

          {step === 'done' ? null : (
            <div className="mt-8 flex items-center justify-between gap-3">
              <div className="text-sm text-muted-foreground">
                {step === 'ai'
                  ? 'This step is optional.'
                  : step === 'microphone'
                    ? 'You can continue without fixing permissions right now.'
                    : 'Continue to the next step.'}
              </div>
              <div className="flex items-center gap-2">
                {step === 'ai' ? (
                  <Button type="button" variant="ghost" onClick={goNext}>
                    Skip
                  </Button>
                ) : null}
                <Button type="button" disabled={!canContinue} onClick={goNext}>
                  Continue
                </Button>
              </div>
            </div>
          )}
        </div>
      </section>
    </div>
  )
}
