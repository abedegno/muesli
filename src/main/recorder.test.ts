import { describe, expect, it, vi } from 'vitest'
import type { AudioCapture, CaptureResult } from './capture/audioCapture'
import { RecordingSession } from './recorder'

class FakeCapture implements AudioCapture {
  private recording = false
  public startCalls = 0
  constructor(private readonly result: CaptureResult) {}
  async start() {
    this.startCalls++
    this.recording = true
  }
  async stop(): Promise<CaptureResult> {
    this.recording = false
    return this.result
  }
  isRecording() {
    return this.recording
  }
}

const sample: CaptureResult = {
  bytes: new Uint8Array([1, 2, 3]),
  mimeType: 'audio/webm;codecs=opus',
  hasSystemAudio: true,
  hasMicAudio: true,
  durationMs: 1234,
}

describe('RecordingSession', () => {
  it('starts the capture and reports recording state', async () => {
    const cap = new FakeCapture(sample)
    const session = new RecordingSession(cap)
    expect(session.isRecording()).toBe(false)
    await session.start()
    expect(session.isRecording()).toBe(true)
    expect(cap.startCalls).toBe(1)
  })

  it('start() is idempotent (no double-start)', async () => {
    const cap = new FakeCapture(sample)
    const session = new RecordingSession(cap)
    await session.start()
    await session.start()
    expect(cap.startCalls).toBe(1)
  })

  it('stop() returns the captured bytes and clears recording state', async () => {
    const cap = new FakeCapture(sample)
    const session = new RecordingSession(cap)
    await session.start()
    const result = await session.stop()
    expect(result.bytes).toEqual(new Uint8Array([1, 2, 3]))
    expect(session.isRecording()).toBe(false)
  })

  it('stop() without start throws', async () => {
    const session = new RecordingSession(new FakeCapture(sample))
    await expect(session.stop()).rejects.toThrow(/not recording/i)
  })

  it('invokes the onWarning hook when system audio is missing', async () => {
    const micOnly: CaptureResult = { ...sample, hasSystemAudio: false }
    const onWarning = vi.fn()
    const session = new RecordingSession(new FakeCapture(micOnly), { onWarning })
    await session.start()
    await session.stop()
    expect(onWarning).toHaveBeenCalledWith(expect.stringMatching(/system audio/i))
  })
})
