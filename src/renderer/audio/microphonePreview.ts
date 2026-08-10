export interface MicrophonePreview {
  stop: () => Promise<void>
}

/** Opens the only microphone stream used while idle and reports its post-gain level. */
export async function startMicrophonePreview({
  deviceId,
  gainLinear,
  onLevel,
}: {
  deviceId?: string
  gainLinear: number
  onLevel: (level: number) => void
}): Promise<MicrophonePreview> {
  const stream = await navigator.mediaDevices.getUserMedia({
    audio: deviceId ? { deviceId: { exact: deviceId } } : true,
    video: false,
  })
  const context = new AudioContext()
  const source = context.createMediaStreamSource(stream)
  const gain = context.createGain()
  const analyser = context.createAnalyser()
  analyser.fftSize = 512
  source.connect(gain)
  gain.gain.value = gainLinear
  gain.connect(analyser)

  const samples = new Float32Array(analyser.fftSize)
  let animationFrame = 0
  let stopped = false
  let lastReportedAt = Number.NEGATIVE_INFINITY
  const readLevel = (timestamp: number) => {
    if (timestamp - lastReportedAt >= 100) {
      analyser.getFloatTimeDomainData(samples)
      onLevel(rmsLevel(samples))
      lastReportedAt = timestamp
    }
    animationFrame = requestAnimationFrame(readLevel)
  }
  animationFrame = requestAnimationFrame(readLevel)

  return {
    stop: async () => {
      if (stopped) return
      stopped = true
      cancelAnimationFrame(animationFrame)
      source.disconnect()
      gain.disconnect()
      analyser.disconnect()
      stream.getTracks().forEach((track) => track.stop())
      await context.close().catch(() => {})
    },
  }
}

export function rmsLevel(samples: Float32Array): number {
  let sum = 0
  for (const sample of samples) sum += sample * sample
  // Typical speech RMS is well below 1. Scale it into a useful meter range.
  return Math.min(1, Math.sqrt(sum / samples.length) * 4)
}
