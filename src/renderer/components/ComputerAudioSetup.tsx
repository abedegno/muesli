import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/Button'
import { t } from '@/i18n'
import type { MuesliBridge } from '../../shared/ipc'

export type ComputerAudioBridge = Pick<
  MuesliBridge,
  'systemAudioAvailable' | 'systemAudioStart' | 'systemAudioStop' | 'systemAudioOpenSettings'
>

export function ComputerAudioSetup({ bridge }: { bridge: ComputerAudioBridge }) {
  const [available, setAvailable] = useState(false)
  const [state, setState] = useState<'idle' | 'checking' | 'ready' | 'unconfirmed'>('idle')
  useEffect(() => {
    let active = true
    void bridge
      .systemAudioAvailable?.()
      .then((value) => {
        if (active) setAvailable(value)
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [bridge])
  if (!available) return null
  return (
    <section
      className="space-y-3 rounded-xl border border-border p-4"
      aria-label={t('audio.computerTitle')}
    >
      <h2 className="text-sm font-medium">{t('audio.computerTitle')}</h2>
      <p className="text-sm leading-6 text-muted-foreground">{t('audio.computerHelp')}</p>
      <Button
        type="button"
        variant="secondary"
        disabled={state === 'checking'}
        onClick={() => {
          setState('checking')
          void (async () => {
            let ready = false
            let captureId: string | undefined
            try {
              const format = await bridge.systemAudioStart()
              ready = format !== null
              captureId = format?.captureId
            } catch {
              /* Report an unconfirmed permission, not a false grant. */
            } finally {
              if (ready) await bridge.systemAudioStop(captureId).catch(() => {})
            }
            setState(ready ? 'ready' : 'unconfirmed')
          })()
        }}
      >
        {t(state === 'checking' ? 'audio.checking' : 'audio.enable')}
      </Button>
      {state === 'ready' && (
        <p role="status" className="text-sm">
          {t('audio.ready')}
        </p>
      )}
      {state === 'unconfirmed' && (
        <div role="status" className="space-y-2 text-sm">
          <p>{t('audio.unconfirmed')}</p>
          <Button
            type="button"
            variant="ghost"
            onClick={() => void bridge.systemAudioOpenSettings().catch(() => {})}
          >
            {t('audio.settings')}
          </Button>
        </div>
      )}
    </section>
  )
}
