import { useEffect, useRef, useState } from 'react'
import { AlertTriangle, Mic, Square } from 'lucide-react'
import { muesli } from '@/api'
import { Button } from '@/components/ui/Button'
import { useAnnouncer } from '@/hooks/useAnnouncer'
import { loadAudioPrefs, saveAudioPrefs } from '@/lib/audioPrefs'
import type { SysAudioStatus } from '../../main/systemAudioPermission'

export type RecordState = 'idle' | 'recording' | 'processing'

function fmt(ms: number): string {
  const total = Math.floor(ms / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

/** Detect macOS from the user agent / platform. */
function isMacOS(): boolean {
  try {
    return (
      navigator.platform?.includes('Mac') ||
      /Mac|iPhone|iPad|iPod/.test(navigator.userAgent)
    )
  } catch {
    return false
  }
}

export function RecordControl({
  state,
  elapsedMs,
  onStart,
  onStop,
  disabledReason,
  onDeviceChange,
  onGainChange,
  micError,
  onRetry,
}: {
  state: RecordState
  elapsedMs: number
  onStart: () => void
  onStop: () => void
  disabledReason?: string
  onDeviceChange?: (deviceId: string | undefined) => void
  onGainChange?: (gainLinear: number) => void
  /** Pass an error object to show the permission-denied or device-invalid panel. */
  micError?: Error | null
  /** Called when the user clicks Retry after a mic error. */
  onRetry?: () => void
}) {
  const { announceAssertive } = useAnnouncer()
  const prevStateRef = useRef<RecordState>(state)

  const prefs = loadAudioPrefs()

  const [devices, setDevices] = useState<MediaDeviceInfo[]>([])
  const [selectedDeviceId, setSelectedDeviceId] = useState<string | undefined>(prefs.deviceId)
  const [gainPct, setGainPct] = useState<number>(Math.round(prefs.gain * 100))
  const [systemAudioStatus, setSystemAudioStatus] = useState<SysAudioStatus | null>(null)

  // Announce recording state changes via the live region.
  useEffect(() => {
    const prev = prevStateRef.current
    prevStateRef.current = state
    if (prev === state) return
    if (state === 'recording') {
      announceAssertive('Recording started')
    } else if (prev === 'recording') {
      announceAssertive('Recording stopped')
    }
  }, [state, announceAssertive])

  // Detect device-invalid errors (unplugged / wrong deviceId) — checked first
  // so they take priority over the more general isPermDenied check.
  const isDeviceInvalid =
    micError != null &&
    ((micError as { code?: string }).code === 'mic-device-invalid' ||
      micError.name === 'MicDeviceInvalidError')

  // Detect permission-denied errors (distinct from device-invalid).
  const isPermDenied =
    !isDeviceInvalid &&
    micError != null &&
    ((micError as { code?: string }).code === 'mic-permission-denied' ||
      micError.name === 'NotAllowedError' ||
      micError.name === 'MicPermissionDeniedError')

  async function enumerateDevices() {
    try {
      if (typeof navigator === 'undefined' || !navigator.mediaDevices?.enumerateDevices) return
      const all = await navigator.mediaDevices.enumerateDevices()
      setDevices(all.filter((d) => d.kind === 'audioinput'))
    } catch {
      /* unavailable in test/SSR env — ignore */
    }
  }

  useEffect(() => {
    void enumerateDevices()
    let cleanup = () => {}
    try {
      if (navigator.mediaDevices?.addEventListener) {
        navigator.mediaDevices.addEventListener('devicechange', enumerateDevices)
        cleanup = () => navigator.mediaDevices.removeEventListener('devicechange', enumerateDevices)
      }
    } catch {
      /* unavailable */
    }
    return cleanup
  }, [])

  useEffect(() => {
    if (!isMacOS()) return
    let cancelled = false
    void (async () => {
      try {
        const status = await muesli.systemAudioStatus?.()
        if (!cancelled) setSystemAudioStatus(status ?? 'unknown')
      } catch {
        if (!cancelled) setSystemAudioStatus('unknown')
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  function handleDeviceChange(e: React.ChangeEvent<HTMLSelectElement>) {
    const val = e.target.value || undefined
    setSelectedDeviceId(val)
    saveAudioPrefs({ deviceId: val, gain: gainPct / 100 })
    onDeviceChange?.(val)
  }

  function handleGainChange(e: React.ChangeEvent<HTMLInputElement>) {
    const pct = Number(e.target.value)
    setGainPct(pct)
    const linear = pct / 100
    saveAudioPrefs({ deviceId: selectedDeviceId, gain: linear })
    onGainChange?.(linear)
  }

  // --- Device-invalid panel ---
  // Shows the device picker so the user can select another mic before retrying.
  // Does NOT show the OS-settings hint (that's only for permission-denied).
  if (isDeviceInvalid) {
    return (
      <div
        role="alert"
        className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/10 p-3 text-sm"
      >
        <p className="font-medium text-destructive">
          The selected microphone is unavailable or was unplugged.
        </p>
        <p className="mt-1 text-muted-foreground">Choose another microphone from the list below.</p>
        <label className="mt-2 flex flex-col gap-1 text-sm">
          <span className="text-muted-foreground">Microphone</span>
          <select
            aria-label="Microphone"
            value={selectedDeviceId ?? ''}
            onChange={handleDeviceChange}
            className="rounded-[var(--radius)] border border-border bg-background px-2 py-1 text-sm"
          >
            <option value="">Default</option>
            {devices.map((d) => (
              <option key={d.deviceId} value={d.deviceId}>
                {d.label || `Microphone ${d.deviceId.slice(0, 6)}`}
              </option>
            ))}
          </select>
        </label>
        <Button
          variant="secondary"
          size="sm"
          className="mt-2"
          onClick={() => {
            void enumerateDevices()
            onRetry?.()
          }}
        >
          Retry
        </Button>
      </div>
    )
  }

  // --- Permission-denied panel ---
  if (isPermDenied) {
    const hint = isMacOS()
      ? 'Open System Settings → Privacy & Security → Microphone and allow this app.'
      : 'Grant microphone permission in your system or browser settings.'
    return (
      <div
        role="alert"
        className="rounded-[var(--radius)] border border-destructive/40 bg-destructive/10 p-3 text-sm"
      >
        <p className="font-medium text-destructive">Microphone access was denied.</p>
        <p className="mt-1 text-muted-foreground">{hint}</p>
        <Button
          variant="secondary"
          size="sm"
          className="mt-2"
          onClick={() => {
            void enumerateDevices()
            onRetry?.()
          }}
        >
          Retry
        </Button>
        <Button
          variant="secondary"
          size="sm"
          className="mt-2 ml-2"
          onClick={() => {
            void muesli.micOpenSettings?.()
          }}
        >
          Open System Settings
        </Button>
      </div>
    )
  }

  const showSystemAudioNotice =
    isMacOS() && (systemAudioStatus === 'denied' || systemAudioStatus === 'restricted')

  const disabled = state !== 'idle'

  const controls = (
    <div className="flex flex-col gap-2">
      {/* Device picker */}
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-muted-foreground">Microphone</span>
        <select
          aria-label="Microphone"
          value={selectedDeviceId ?? ''}
          onChange={handleDeviceChange}
          disabled={disabled}
          className="rounded-[var(--radius)] border border-border bg-background px-2 py-1 text-sm disabled:opacity-50"
        >
          <option value="">Default</option>
          {devices.map((d) => (
            <option key={d.deviceId} value={d.deviceId}>
              {d.label || `Microphone ${d.deviceId.slice(0, 6)}`}
            </option>
          ))}
        </select>
      </label>

      {/* Gain slider */}
      <label className="flex flex-col gap-1 text-sm">
        <span className="text-muted-foreground">Gain</span>
        <div className="flex items-center gap-2">
          <input
            type="range"
            aria-label="Gain"
            min={0}
            max={200}
            step={1}
            value={gainPct}
            onChange={handleGainChange}
            disabled={disabled}
            className="flex-1 disabled:opacity-50"
          />
          <span className="w-12 text-right tabular-nums">{gainPct}%</span>
        </div>
      </label>
    </div>
  )

  if (state === 'idle') {
    if (disabledReason) {
      return (
        <div className="flex flex-col gap-3">
          {controls}
          <div className="flex items-center gap-1.5 text-sm text-amber-500" role="status" aria-live="polite">
            <AlertTriangle size={16} aria-hidden />
            <span>{disabledReason}</span>
          </div>
        </div>
      )
    }
    return (
      <div className="flex flex-col gap-3">
        {showSystemAudioNotice ? (
          <div
            role="alert"
            className="rounded-[var(--radius)] border border-amber-500/40 bg-amber-500/10 p-3 text-sm"
          >
            <p className="font-medium text-amber-600">System audio capture is not enabled.</p>
            <p className="mt-1 text-muted-foreground">
              You can still record microphone-only. Open System Settings to grant access if you want both sides of the call.
            </p>
            <Button
              variant="secondary"
              size="sm"
              className="mt-2"
              onClick={() => {
                void muesli.systemAudioOpenSettings?.()
              }}
            >
              Open System Settings
            </Button>
          </div>
        ) : null}
        {controls}
        <Button variant="primary" onClick={onStart}>
          <Mic size={18} /> Record
        </Button>
      </div>
    )
  }
  if (state === 'recording') {
    return (
      <div className="flex flex-col gap-3">
        {showSystemAudioNotice ? (
          <div
            role="alert"
            className="rounded-[var(--radius)] border border-amber-500/40 bg-amber-500/10 p-3 text-sm"
          >
            <p className="font-medium text-amber-600">System audio capture is not enabled.</p>
            <p className="mt-1 text-muted-foreground">
              You can still record microphone-only. Open System Settings to grant access if you want both sides of the call.
            </p>
            <Button
              variant="secondary"
              size="sm"
              className="mt-2"
              onClick={() => {
                void muesli.systemAudioOpenSettings?.()
              }}
            >
              Open System Settings
            </Button>
          </div>
        ) : null}
        {controls}
        <div className="flex items-center gap-3" role="status" aria-live="polite">
          <span className="h-2.5 w-2.5 animate-pulse rounded-full bg-destructive" aria-hidden />
          <span className="font-mono text-sm tabular-nums">{fmt(elapsedMs)}</span>
          <span className="sr-only">Recording</span>
          <Button variant="secondary" size="sm" onClick={onStop}>
            <Square size={14} /> Stop
          </Button>
        </div>
      </div>
    )
  }
  return (
    <div className="flex items-center gap-2 text-sm text-muted-foreground" role="status" aria-live="polite">
      <span className="h-3 w-3 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" aria-hidden />
      Processing…
    </div>
  )
}
