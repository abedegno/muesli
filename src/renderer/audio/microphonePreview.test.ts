// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { startMicrophonePreview } from './microphonePreview'

describe('startMicrophonePreview', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('applies gain and tears down its stream, graph, frame, and context', async () => {
    const track = { stop: vi.fn() }
    const stream = { getTracks: () => [track] } as unknown as MediaStream
    const source = { connect: vi.fn(), disconnect: vi.fn() }
    const gain = { gain: { value: 0 }, connect: vi.fn(), disconnect: vi.fn() }
    const analyser = {
      fftSize: 0,
      getFloatTimeDomainData: vi.fn(),
      disconnect: vi.fn(),
    }
    const context = {
      createMediaStreamSource: vi.fn(() => source),
      createGain: vi.fn(() => gain),
      createAnalyser: vi.fn(() => analyser),
      close: vi.fn().mockResolvedValue(undefined),
    }
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getUserMedia: vi.fn().mockResolvedValue(stream) },
    })
    vi.stubGlobal('AudioContext', vi.fn(() => context))
    vi.stubGlobal('requestAnimationFrame', vi.fn(() => 7))
    vi.stubGlobal('cancelAnimationFrame', vi.fn())

    const preview = await startMicrophonePreview({
      deviceId: 'usb-mic',
      gainLinear: 1.5,
      onLevel: vi.fn(),
    })
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledWith({
      audio: { deviceId: { exact: 'usb-mic' } },
      video: false,
    })
    expect(source.connect).toHaveBeenCalledWith(gain)
    expect(gain.gain.value).toBe(1.5)
    expect(gain.connect).toHaveBeenCalledWith(analyser)

    await preview.stop()
    expect(track.stop).toHaveBeenCalledOnce()
    expect(cancelAnimationFrame).toHaveBeenCalledWith(7)
    expect(context.close).toHaveBeenCalledOnce()
  })
})

