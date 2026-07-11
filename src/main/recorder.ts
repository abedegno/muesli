import type { AudioCapture, CaptureResult } from './capture/audioCapture'

interface RecordingSessionOptions {
  // Called with a human-readable warning, e.g. when system audio couldn't be captured.
  onWarning?: (message: string) => void
}

// RecordingSession is a thin, testable orchestrator over an AudioCapture. It
// guards the start/stop lifecycle and surfaces capture-quality warnings to the
// UI. It deliberately holds no Electron/DOM types so it unit-tests in Node.
export class RecordingSession {
  private recording = false

  constructor(
    private readonly capture: AudioCapture,
    private readonly opts: RecordingSessionOptions = {},
  ) {}

  isRecording(): boolean {
    return this.recording
  }

  async start(): Promise<void> {
    if (this.recording) return // idempotent
    await this.capture.start()
    this.recording = true
  }

  async stop(): Promise<CaptureResult> {
    if (!this.recording) {
      throw new Error('not recording')
    }
    const result = await this.capture.stop()
    this.recording = false
    if (!result.hasSystemAudio) {
      this.opts.onWarning?.(
        'System audio was not captured (mic only). See per-OS limitations.',
      )
    }
    return result
  }
}
