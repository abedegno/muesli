// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ElectronCapture, MicPermissionDeniedError, MicDeviceInvalidError } from './electronCapture'

// ---------------------------------------------------------------------------
// Minimal AudioContext / MediaRecorder stubs
// ---------------------------------------------------------------------------

function makeMicStream() {
  return { getTracks: () => [{ stop: vi.fn() }] } as unknown as MediaStream
}

function makeGainNode(gainValue = 0) {
  const node = {
    gain: { value: gainValue },
    connect: vi.fn(),
  }
  return node
}

function makeMediaStreamSource() {
  return { connect: vi.fn() }
}

function makeDestination() {
  return {
    stream: {
      getTracks: () => [],
    } as unknown as MediaStream,
  }
}

class TestBlob {
  readonly type: string

  constructor(_parts: BlobPart[], options?: BlobPropertyBag) {
    this.type = options?.type ?? ''
  }

  async arrayBuffer(): Promise<ArrayBuffer> {
    return new ArrayBuffer(0)
  }
}

function makeAudioContext(gainNode: ReturnType<typeof makeGainNode>) {
  const source = makeMediaStreamSource()
  const destination = makeDestination()
  const merger = { connect: vi.fn() }
  const ctx = {
    createMediaStreamSource: vi.fn(() => source),
    createGain: vi.fn(() => gainNode),
    createMediaStreamDestination: vi.fn(() => destination),
    createChannelMerger: vi.fn(() => merger),
    createAnalyser: vi.fn(() => ({
      fftSize: 0,
      getFloatTimeDomainData: vi.fn((samples: Float32Array) => samples.fill(0.25)),
      disconnect: vi.fn(),
    })),
    close: vi.fn().mockResolvedValue(undefined),
    source,
    destination,
    merger,
  }
  return ctx
}

// Stub MediaRecorder globally
function makeMediaRecorder() {
  const rec: {
    ondataavailable: ((e: { data: { size: number } }) => void) | null
    onstop: (() => void) | null
    state: string
    start: ReturnType<typeof vi.fn>
    stop: ReturnType<typeof vi.fn>
  } = {
    ondataavailable: null,
    onstop: null,
    state: 'recording',
    start: vi.fn(),
    stop: vi.fn(function (this: typeof rec) {
      if (this.onstop) this.onstop()
    }),
  }
  return rec
}

beforeEach(() => {
  vi.restoreAllMocks()
  vi.stubGlobal('Blob', TestBlob as unknown as typeof Blob)
})

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ElectronCapture', () => {
  it('reports post-gain microphone levels from an analyser attached after the gain node', async () => {
    const gainNode = makeGainNode()
    const ctx = makeAudioContext(gainNode)
    const rec = makeMediaRecorder()
    let animationCallback: FrameRequestCallback | undefined
    vi.stubGlobal(
      'requestAnimationFrame',
      vi.fn((callback: FrameRequestCallback) => {
        animationCallback = callback
        return 1
      })
    )
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
    vi.stubGlobal(
      'AudioContext',
      vi.fn(() => ctx)
    )
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        vi.fn(() => rec),
        {
          isTypeSupported: vi.fn(() => true),
        }
      )
    )
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(makeMicStream()) },
    })
    const onLevel = vi.fn()

    const capture = new ElectronCapture({ gainLinear: 0.5, onLevel })
    await capture.start()
    animationCallback?.(0)

    const analyser = ctx.createAnalyser.mock.results[0].value
    expect(gainNode.connect).toHaveBeenCalledWith(analyser)
    expect(onLevel).toHaveBeenCalledWith(1)
    await capture.stop()
    expect(cancelAnimationFrame).toHaveBeenCalled()
  })

  it('forwards deviceId constraint to getUserMedia', async () => {
    const gainNode = makeGainNode()
    const ctx = makeAudioContext(gainNode)
    const rec = makeMediaRecorder()

    vi.stubGlobal(
      'AudioContext',
      vi.fn(() => ctx)
    )
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        vi.fn(() => rec),
        {
          isTypeSupported: vi.fn(() => true),
        }
      )
    )

    const getUserMedia = vi.fn().mockResolvedValue(makeMicStream())
    const getDisplayMedia = vi.fn().mockRejectedValue(new Error('no display'))
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia, getDisplayMedia },
    })

    const capture = new ElectronCapture({ deviceId: 'dev-abc', gainLinear: 1.0 })
    await capture.start()

    expect(getUserMedia).toHaveBeenCalledWith({
      audio: { deviceId: { exact: 'dev-abc' } },
      video: false,
    })
  })

  it('creates a GainNode, connects it between source and destination, and sets gain.value', async () => {
    const gainNode = makeGainNode()
    const ctx = makeAudioContext(gainNode)
    const rec = makeMediaRecorder()

    vi.stubGlobal(
      'AudioContext',
      vi.fn(() => ctx)
    )
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        vi.fn(() => rec),
        {
          isTypeSupported: vi.fn(() => true),
        }
      )
    )

    vi.stubGlobal('navigator', {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue(makeMicStream()),
      },
    })

    const capture = new ElectronCapture({ gainLinear: 0.5 })
    await capture.start()

    expect(ctx.createGain).toHaveBeenCalledOnce()
    expect(gainNode.gain.value).toBe(0.5)
    expect(ctx.createChannelMerger).not.toHaveBeenCalled()
    expect(ctx.source.connect).toHaveBeenCalledWith(gainNode)
    expect(gainNode.connect).toHaveBeenCalledWith(ctx.destination)

    const result = await capture.stop()
    expect(result.hasSystemAudio).toBe(false)
  })

  it('routes mic to merger input 0 and system to merger input 1 when a system stream is provided', async () => {
    const gainNode = makeGainNode()
    const ctx = makeAudioContext(gainNode)
    const rec = makeMediaRecorder()

    vi.stubGlobal(
      'AudioContext',
      vi.fn(() => ctx)
    )
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        vi.fn(() => rec),
        {
          isTypeSupported: vi.fn(() => true),
        }
      )
    )

    vi.stubGlobal('navigator', {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue(makeMicStream()),
      },
    })

    const capture = new ElectronCapture({
      getSystemAudioStream: async () => makeMicStream(),
    })
    await capture.start()

    expect(ctx.createChannelMerger).toHaveBeenCalledWith(2)
    expect(gainNode.connect).toHaveBeenCalledWith(ctx.merger, 0, 0)
    expect(ctx.source.connect).toHaveBeenCalledWith(ctx.merger, 0, 1)
    expect(ctx.merger.connect).toHaveBeenCalledWith(ctx.destination)

    const result = await capture.stop()
    expect(result.hasSystemAudio).toBe(true)
  })

  it('falls back to mono mic and hasSystemAudio=false when no system stream', async () => {
    const gainNode = makeGainNode()
    const ctx = makeAudioContext(gainNode)
    const rec = makeMediaRecorder()

    vi.stubGlobal(
      'AudioContext',
      vi.fn(() => ctx)
    )
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        vi.fn(() => rec),
        {
          isTypeSupported: vi.fn(() => true),
        }
      )
    )

    vi.stubGlobal('navigator', {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue(makeMicStream()),
      },
    })

    const capture = new ElectronCapture({
      getSystemAudioStream: async () => null,
    })
    await capture.start()

    expect(ctx.createChannelMerger).not.toHaveBeenCalled()
    expect(gainNode.connect).toHaveBeenCalledWith(ctx.destination)

    const result = await capture.stop()
    expect(result.hasSystemAudio).toBe(false)
  })

  it('treats a throwing provider as no system audio', async () => {
    const gainNode = makeGainNode()
    const ctx = makeAudioContext(gainNode)
    const rec = makeMediaRecorder()

    vi.stubGlobal(
      'AudioContext',
      vi.fn(() => ctx)
    )
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        vi.fn(() => rec),
        {
          isTypeSupported: vi.fn(() => true),
        }
      )
    )

    vi.stubGlobal('navigator', {
      mediaDevices: {
        getUserMedia: vi.fn().mockResolvedValue(makeMicStream()),
      },
    })

    const capture = new ElectronCapture({
      getSystemAudioStream: async () => {
        throw new Error('x')
      },
    })
    await capture.start()

    expect(ctx.createChannelMerger).not.toHaveBeenCalled()
    expect(gainNode.connect).toHaveBeenCalledWith(ctx.destination)

    const result = await capture.stop()
    expect(result.hasSystemAudio).toBe(false)
  })

  it('captures system audio only when mic fails with an unmatched error', async () => {
    const gainNode = makeGainNode()
    const ctx = makeAudioContext(gainNode)
    const rec = makeMediaRecorder()

    vi.stubGlobal(
      'AudioContext',
      vi.fn(() => ctx)
    )
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        vi.fn(() => rec),
        {
          isTypeSupported: vi.fn(() => true),
        }
      )
    )

    const notReadable = new Error('Mic busy')
    notReadable.name = 'NotReadableError'

    vi.stubGlobal('navigator', {
      mediaDevices: {
        getUserMedia: vi.fn().mockRejectedValue(notReadable),
      },
    })

    const capture = new ElectronCapture({
      getSystemAudioStream: async () => makeMicStream(),
    })
    await capture.start()

    expect(ctx.createChannelMerger).not.toHaveBeenCalled()
    expect(ctx.source.connect).toHaveBeenCalledWith(ctx.destination)

    const result = await capture.stop()
    expect(result.hasMicAudio).toBe(false)
    expect(result.hasSystemAudio).toBe(true)
  })

  it('throws MicPermissionDeniedError (code mic-permission-denied) when getUserMedia rejects with NotAllowedError', async () => {
    const gainNode = makeGainNode()
    const ctx = makeAudioContext(gainNode)
    const rec = makeMediaRecorder()

    vi.stubGlobal(
      'AudioContext',
      vi.fn(() => ctx)
    )
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        vi.fn(() => rec),
        {
          isTypeSupported: vi.fn(() => true),
        }
      )
    )

    const notAllowed = new Error('Permission denied')
    notAllowed.name = 'NotAllowedError'

    vi.stubGlobal('navigator', {
      mediaDevices: {
        getUserMedia: vi.fn().mockRejectedValue(notAllowed),
      },
    })

    const capture = new ElectronCapture()
    await expect(capture.start()).rejects.toSatisfy((err: unknown) => {
      return (
        err instanceof MicPermissionDeniedError &&
        err.code === 'mic-permission-denied' &&
        err.name === 'MicPermissionDeniedError'
      )
    })
  })

  it('closes the AudioContext after NotAllowedError to prevent leaks', async () => {
    const gainNode = makeGainNode()
    const ctx = makeAudioContext(gainNode)
    ctx.close = vi.fn().mockResolvedValue(undefined)

    vi.stubGlobal(
      'AudioContext',
      vi.fn(() => ctx)
    )
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        vi.fn(() => makeMediaRecorder()),
        {
          isTypeSupported: vi.fn(() => true),
        }
      )
    )

    const notAllowed = new Error('Permission denied')
    notAllowed.name = 'NotAllowedError'

    vi.stubGlobal('navigator', {
      mediaDevices: {
        getUserMedia: vi.fn().mockRejectedValue(notAllowed),
      },
    })

    const capture = new ElectronCapture()
    await expect(capture.start()).rejects.toBeInstanceOf(MicPermissionDeniedError)
    expect(ctx.close).toHaveBeenCalledOnce()
  })

  it('throws MicDeviceInvalidError when getUserMedia rejects with OverconstrainedError and closes AudioContext', async () => {
    const gainNode = makeGainNode()
    const ctx = makeAudioContext(gainNode)
    ctx.close = vi.fn().mockResolvedValue(undefined)

    vi.stubGlobal(
      'AudioContext',
      vi.fn(() => ctx)
    )
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        vi.fn(() => makeMediaRecorder()),
        {
          isTypeSupported: vi.fn(() => true),
        }
      )
    )

    const overconstrained = new Error('Overconstrained')
    overconstrained.name = 'OverconstrainedError'

    vi.stubGlobal('navigator', {
      mediaDevices: {
        getUserMedia: vi.fn().mockRejectedValue(overconstrained),
      },
    })

    const capture = new ElectronCapture({ deviceId: 'stale-device-id' })
    await expect(capture.start()).rejects.toSatisfy((err: unknown) => {
      return (
        err instanceof MicDeviceInvalidError &&
        err.code === 'mic-device-invalid' &&
        err.name === 'MicDeviceInvalidError'
      )
    })
    expect(ctx.close).toHaveBeenCalledOnce()
  })

  it('throws MicDeviceInvalidError when getUserMedia rejects with NotFoundError and closes AudioContext', async () => {
    const gainNode = makeGainNode()
    const ctx = makeAudioContext(gainNode)
    ctx.close = vi.fn().mockResolvedValue(undefined)

    vi.stubGlobal(
      'AudioContext',
      vi.fn(() => ctx)
    )
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        vi.fn(() => makeMediaRecorder()),
        {
          isTypeSupported: vi.fn(() => true),
        }
      )
    )

    const notFound = new Error('Device not found')
    notFound.name = 'NotFoundError'

    vi.stubGlobal('navigator', {
      mediaDevices: {
        getUserMedia: vi.fn().mockRejectedValue(notFound),
      },
    })

    const capture = new ElectronCapture({ deviceId: 'unplugged-device' })
    await expect(capture.start()).rejects.toSatisfy((err: unknown) => {
      return (
        err instanceof MicDeviceInvalidError &&
        err.code === 'mic-device-invalid' &&
        err.name === 'MicDeviceInvalidError'
      )
    })
    expect(ctx.close).toHaveBeenCalledOnce()
  })
})

it('stops capture at the two-hour safety limit and reports it for saving', async () => {
  vi.useFakeTimers()
  try {
    const ctx = makeAudioContext(makeGainNode()),
      rec = makeMediaRecorder(),
      onLimit = vi.fn()
    vi.stubGlobal(
      'AudioContext',
      vi.fn(() => ctx)
    )
    vi.stubGlobal(
      'MediaRecorder',
      Object.assign(
        vi.fn(() => rec),
        { isTypeSupported: () => true }
      )
    )
    vi.stubGlobal('navigator', {
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(makeMicStream()) },
    })
    const capture = new ElectronCapture({ onLimit } as never)
    await capture.start()
    await vi.advanceTimersByTimeAsync(2 * 60 * 60_000)
    expect(onLimit).toHaveBeenCalledTimes(1)
    expect(rec.stop).toHaveBeenCalledTimes(1)
    await capture.stop()
    expect(ctx.close).toHaveBeenCalledTimes(1)
  } finally {
    vi.useRealTimers()
  }
})

it('retains the chunk that reaches the stop threshold instead of silently truncating it', async () => {
  const ctx = makeAudioContext(makeGainNode()),
    rec = makeMediaRecorder(),
    onLimit = vi.fn()
  let recorded: BlobPart[] = []
  vi.stubGlobal(
    'AudioContext',
    vi.fn(() => ctx)
  )
  vi.stubGlobal(
    'MediaRecorder',
    Object.assign(
      vi.fn(() => rec),
      { isTypeSupported: () => true }
    )
  )
  vi.stubGlobal('navigator', {
    mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(makeMicStream()) },
  })
  vi.stubGlobal(
    'Blob',
    class extends TestBlob {
      constructor(parts: BlobPart[], options?: BlobPropertyBag) {
        super(parts, options)
        recorded = [...parts]
      }
    }
  )
  const capture = new ElectronCapture({ onLimit })
  await capture.start()
  const first = { size: 60 * 1024 * 1024 },
    last = { size: 8 * 1024 * 1024 }
  rec.ondataavailable?.({ data: first })
  expect(onLimit).not.toHaveBeenCalled()
  rec.ondataavailable?.({ data: last })
  expect(onLimit).toHaveBeenCalledOnce()
  await capture.stop()
  expect(recorded).toEqual([first, last])
  expect(ctx.close).toHaveBeenCalled()
})
