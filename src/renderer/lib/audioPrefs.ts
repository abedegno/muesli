// audioPrefs — persists the user's preferred audio input device and capture
// gain to localStorage so selections survive app restarts.

export interface AudioPrefs {
  deviceId: string | undefined
  gain: number
}

const DEVICE_KEY = 'muesli.audio.deviceId'
const GAIN_KEY = 'muesli.audio.gain'

const DEFAULTS: AudioPrefs = { deviceId: undefined, gain: 1.0 }

/** Read persisted audio prefs; returns sensible defaults when absent or unavailable. */
export function loadAudioPrefs(): AudioPrefs {
  try {
    const deviceId = localStorage.getItem(DEVICE_KEY) ?? undefined
    const gainRaw = localStorage.getItem(GAIN_KEY)
    const gain = gainRaw !== null ? parseFloat(gainRaw) : DEFAULTS.gain
    return {
      deviceId: deviceId || undefined,
      gain: Number.isFinite(gain) ? gain : DEFAULTS.gain,
    }
  } catch {
    return { ...DEFAULTS }
  }
}

/** Persist audio prefs; failures (storage unavailable) are swallowed. */
export function saveAudioPrefs(prefs: AudioPrefs): void {
  try {
    if (prefs.deviceId != null) {
      localStorage.setItem(DEVICE_KEY, prefs.deviceId)
    } else {
      localStorage.removeItem(DEVICE_KEY)
    }
    localStorage.setItem(GAIN_KEY, String(prefs.gain))
  } catch {
    /* storage unavailable — keep in-memory value */
  }
}
