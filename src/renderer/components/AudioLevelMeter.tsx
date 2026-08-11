import { useEffect, useRef, useState } from 'react'

export const SUSTAINED_SILENCE_MS = 30_000
const SILENCE_LEVEL = 0.01

export function AudioLevelMeter({
  level,
  active = true,
  silenceThresholdMs = SUSTAINED_SILENCE_MS,
}: {
  /** Normalized post-gain signal level, from 0 (silent) to 1 (maximum). */
  level: number
  active?: boolean
  silenceThresholdMs?: number
}) {
  const [silent, setSilent] = useState(false)
  const silenceStartedAt = useRef<number | null>(null)
  const clampedLevel = Math.max(0, Math.min(1, level))

  useEffect(() => {
    if (!active || clampedLevel > SILENCE_LEVEL) {
      silenceStartedAt.current = null
      setSilent(false)
      return
    }

    if (silenceStartedAt.current === null) silenceStartedAt.current = Date.now()
    const remaining = silenceThresholdMs - (Date.now() - silenceStartedAt.current)
    if (remaining <= 0) {
      setSilent(true)
      return
    }

    const timer = window.setTimeout(() => setSilent(true), remaining)
    return () => window.clearTimeout(timer)
  }, [active, clampedLevel, silenceThresholdMs])

  const state = !active ? 'Inactive' : silent ? 'No sound detected' : clampedLevel > SILENCE_LEVEL ? 'Sound detected' : 'Listening…'

  return (
    <div className="flex items-center gap-2 text-xs" data-testid="microphone-level-meter">
      <div
        className="h-2 flex-1 overflow-hidden rounded-full bg-muted"
        role="meter"
        aria-label="Microphone level"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(clampedLevel * 100)}
      >
        <div
          className="h-full bg-primary transition-[width] duration-100"
          style={{ width: `${Math.round(clampedLevel * 100)}%` }}
        />
      </div>
      <span className="w-28 text-right text-muted-foreground" aria-live="polite">
        {state}
      </span>
    </div>
  )
}

