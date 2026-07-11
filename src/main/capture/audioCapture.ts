// A captured recording: the encoded bytes plus what we managed to capture.
export interface CaptureResult {
  bytes: Uint8Array
  mimeType: string // e.g. 'audio/webm;codecs=opus'
  hasSystemAudio: boolean
  hasMicAudio: boolean
  durationMs: number
}

// AudioCapture abstracts per-OS recording. start() begins capture; stop()
// finalises and returns the mixed recording. Implementations: ElectronCapture
// (v1) and future native Core Audio / WASAPI / PipeWire modules (backlog).
export interface AudioCapture {
  start(): Promise<void>
  stop(): Promise<CaptureResult>
  isRecording(): boolean
}
