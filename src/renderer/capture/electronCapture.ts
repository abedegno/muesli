import type { AudioCapture, CaptureResult } from '../../main/capture/audioCapture'
import { PcmFrameEncoder } from '../../shared/pcm'

// ElectronCapture (v1): microphone via getUserMedia, optional system audio via
// an injected provider. Both streams are mixed in a single AudioContext and
// encoded to WebM/Opus with MediaRecorder.
//
// When system audio is unavailable we record mic-only and set hasSystemAudio=
// false so the UI can warn the user.

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
  /** Optional provider for a system-audio MediaStream. */
  getSystemAudioStream?: () => Promise<MediaStream | null>
  /** Optional PCM callback used by the live transcript stream. */
  onPcmFrame?: (frame: ArrayBuffer) => void
}

export class ElectronCapture implements AudioCapture {
  private recorder?: MediaRecorder
  private chunks: BlobPart[] = []
  private streams: MediaStream[] = []
  private audioCtx?: AudioContext
  private pcmEncoder?: PcmFrameEncoder
  private pcmTap?: ScriptProcessorNode
  private pcmTapSink?: GainNode
  private startedAt = 0
  private hasSystem = false
  private hasMic = false

  private readonly deviceId?: string
  private readonly gainLinear: number
  private readonly getSystemAudioStream?: () => Promise<MediaStream | null>
  private readonly onPcmFrame?: (frame: ArrayBuffer) => void

  constructor(config: ElectronCaptureConfig = {}) {
    this.deviceId = config.deviceId
    this.gainLinear = config.gainLinear ?? 1.0
    this.getSystemAudioStream = config.getSystemAudioStream
    this.onPcmFrame = config.onPcmFrame
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
    let gainNode: GainNode | undefined
    this.pcmEncoder = this.onPcmFrame ? new PcmFrameEncoder({ inputSampleRate: ctx.sampleRate }) : undefined
    if (this.pcmEncoder && this.onPcmFrame) {
      const tap = ctx.createScriptProcessor(4096, 2, 1)
      const tapSink = ctx.createGain()
      tapSink.gain.value = 0
      tap.onaudioprocess = (event) => {
        try {
          const input = event.inputBuffer
          const mono = mixAudioBufferToMono(input)
          const frames = this.pcmEncoder?.push(mono) ?? []
          for (const frame of frames) {
            this.onPcmFrame?.(new Uint8Array(frame).buffer)
          }
        } catch {
          // Best-effort live transcript only.
        }
      }
      tap.connect(tapSink)
      tapSink.connect(destination)
      this.pcmTap = tap
      this.pcmTapSink = tapSink
    }

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
      gainNode = ctx.createGain()
      gainNode.gain.value = this.gainLinear
      micSource.connect(gainNode)
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

    let systemStream: MediaStream | null = null
    try {
      systemStream = (await this.getSystemAudioStream?.()) ?? null
    } catch {
      systemStream = null
    }

    if (systemStream && this.hasMic && gainNode) {
      this.streams.push(systemStream)
      const systemSource = ctx.createMediaStreamSource(systemStream)
      const merger = ctx.createChannelMerger(2)
      gainNode.connect(merger, 0, 0) // mic -> left
      systemSource.connect(merger, 0, 1) // system -> right
      merger.connect(destination)
      if (this.pcmTap) merger.connect(this.pcmTap)
      this.hasSystem = true
    } else if (systemStream && !this.hasMic) {
      this.streams.push(systemStream)
      const systemSource = ctx.createMediaStreamSource(systemStream)
      systemSource.connect(destination)
      if (this.pcmTap) systemSource.connect(this.pcmTap)
      this.hasSystem = true
    } else if (this.hasMic && gainNode) {
      gainNode.connect(destination) // mono fallback (today's behavior)
      if (this.pcmTap) gainNode.connect(this.pcmTap)
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
    this.pcmTap?.disconnect()
    this.pcmTapSink?.disconnect()
    await this.audioCtx?.close()
    this.pcmTap = undefined
    this.pcmTapSink = undefined
    this.pcmEncoder = undefined

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

function mixAudioBufferToMono(buffer: AudioBuffer): Float32Array {
  const { length, numberOfChannels } = buffer
  const mono = new Float32Array(length)
  const channels: Float32Array[] = []
  for (let i = 0; i < numberOfChannels; i++) {
    channels.push(buffer.getChannelData(i))
  }
  for (let i = 0; i < length; i++) {
    let sum = 0
    for (let c = 0; c < channels.length; c++) {
      sum += channels[c][i] ?? 0
    }
    mono[i] = channels.length > 0 ? sum / channels.length : 0
  }
  return mono
}
