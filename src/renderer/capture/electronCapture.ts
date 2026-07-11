import type { AudioCapture, CaptureResult } from '../../main/capture/audioCapture'

// ElectronCapture (v1): microphone via getUserMedia, system audio via
// getDisplayMedia (Chromium loopback where the OS allows it). Both streams are
// mixed in a single AudioContext and encoded to WebM/Opus with MediaRecorder.
//
// LIMITATIONS (see plan + backlog): system-audio loopback is reliable on
// Windows, partial on macOS (screen-share audio only / needs a loopback driver),
// and PipeWire-dependent on Linux. When system audio is unavailable we record
// mic-only and set hasSystemAudio=false so the UI can warn the user.

/** Thrown when the user denies microphone permission (NotAllowedError). */
export class MicPermissionDeniedError extends Error {
  readonly code = 'mic-permission-denied' as const
  constructor() {
    super('Microphone access was denied.')
    this.name = 'MicPermissionDeniedError'
  }
}

/** Thrown when the persisted mic device is unavailable or unplugged (OverconstrainedError / NotFoundError). */
export class MicDeviceInvalidError extends Error {
  readonly code = 'mic-device-invalid' as const
  constructor() {
    super('The selected microphone is unavailable or was unplugged.')
    this.name = 'MicDeviceInvalidError'
  }
}

export interface ElectronCaptureConfig {
  /** Specific audio input device to record from. */
  deviceId?: string
  /** Linear gain multiplier applied to the mic (0 = silent, 1 = unity, 2 = double). Defaults to 1.0. */
  gainLinear?: number
}

export class ElectronCapture implements AudioCapture {
  private recorder?: MediaRecorder
  private chunks: BlobPart[] = []
  private streams: MediaStream[] = []
  private audioCtx?: AudioContext
  private startedAt = 0
  private hasSystem = false
  private hasMic = false

  private readonly deviceId?: string
  private readonly gainLinear: number

  constructor(config: ElectronCaptureConfig = {}) {
    this.deviceId = config.deviceId
    this.gainLinear = config.gainLinear ?? 1.0
  }

  isRecording(): boolean {
    return this.recorder?.state === 'recording'
  }

  async start(): Promise<void> {
    this.chunks = []
    this.streams = []
    const ctx = new AudioContext()
    this.audioCtx = ctx
    const destination = ctx.createMediaStreamDestination()

    // Microphone. If the user denies permission we surface a typed error so
    // the UI can show a helpful recovery hint instead of a generic message.
    // If the persisted device is unplugged/invalid we surface MicDeviceInvalidError
    // so the UI can prompt the user to select a different device.
    try {
      const audioConstraints: MediaTrackConstraints = this.deviceId
        ? { deviceId: { exact: this.deviceId } }
        : true as unknown as MediaTrackConstraints
      const mic = await navigator.mediaDevices.getUserMedia({
        audio: audioConstraints,
        video: false,
      })
      this.streams.push(mic)
      const micSource = ctx.createMediaStreamSource(mic)
      const gainNode = ctx.createGain()
      gainNode.gain.value = this.gainLinear
      micSource.connect(gainNode)
      gainNode.connect(destination)
      this.hasMic = true
    } catch (err) {
      if (err instanceof Error && err.name === 'NotAllowedError') {
        await this.audioCtx?.close().catch(() => {})  // clean up leaking context
        this.audioCtx = undefined
        throw new MicPermissionDeniedError()
      }
      if (err instanceof Error && (err.name === 'OverconstrainedError' || err.name === 'NotFoundError')) {
        await this.audioCtx?.close().catch(() => {})
        this.audioCtx = undefined
        throw new MicDeviceInvalidError()
      }
      this.hasMic = false
    }

    // System audio (best-effort; Chromium requires a video track to grant audio,
    // so we request video then immediately drop its track).
    try {
      const display = await navigator.mediaDevices.getDisplayMedia({
        audio: true,
        video: true,
      })
      this.streams.push(display)
      display.getVideoTracks().forEach((t) => {
        t.stop()
        display.removeTrack(t)
      })
      if (display.getAudioTracks().length > 0) {
        ctx.createMediaStreamSource(display).connect(destination)
        this.hasSystem = true
      }
    } catch {
      this.hasSystem = false
    }

    if (!this.hasMic && !this.hasSystem) {
      throw new Error('no audio sources available (mic and system both unavailable)')
    }

    const mimeType = MediaRecorder.isTypeSupported('audio/webm;codecs=opus')
      ? 'audio/webm;codecs=opus'
      : 'audio/webm'
    const recorder = new MediaRecorder(destination.stream, { mimeType })
    recorder.ondataavailable = (e) => {
      if (e.data.size > 0) this.chunks.push(e.data)
    }
    recorder.start(1000) // 1s timeslices so long meetings flush incrementally
    this.recorder = recorder
    this.startedAt = Date.now()
  }

  async stop(): Promise<CaptureResult> {
    const recorder = this.recorder
    if (!recorder) throw new Error('not recording')

    const blob = await new Promise<Blob>((resolve) => {
      recorder.onstop = () => resolve(new Blob(this.chunks, { type: recorder.mimeType }))
      recorder.stop()
    })

    this.streams.forEach((s) => s.getTracks().forEach((t) => t.stop()))
    await this.audioCtx?.close()

    const bytes = new Uint8Array(await blob.arrayBuffer())
    return {
      bytes,
      mimeType: recorder.mimeType,
      hasSystemAudio: this.hasSystem,
      hasMicAudio: this.hasMic,
      durationMs: Date.now() - this.startedAt,
    }
  }
}
