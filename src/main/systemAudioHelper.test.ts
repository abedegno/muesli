import { EventEmitter } from 'node:events'
import { PassThrough } from 'node:stream'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { spawnMock } = vi.hoisted(() => ({
  spawnMock: vi.fn(),
}))

vi.mock('node:child_process', () => ({
  spawn: spawnMock,
}))

type FakeChild = EventEmitter & {
  stdout: PassThrough
  stderr: PassThrough
  killed: boolean
  kill: ReturnType<typeof vi.fn>
}

function createFakeChild() {
  const child = new EventEmitter() as FakeChild
  child.stdout = new PassThrough()
  child.stderr = new PassThrough()
  child.killed = false
  child.kill = vi.fn((signal?: NodeJS.Signals | number) => {
    if (signal === 'SIGTERM') {
      child.killed = true
      queueMicrotask(() => child.emit('exit', 0, 'SIGTERM'))
    }
    return true
  })
  return child
}

async function loadHelper() {
  return await import('./systemAudioHelper')
}

describe('systemAudioHelper', () => {
  beforeEach(() => {
    spawnMock.mockReset()
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('reports unavailable on non-macOS', async () => {
    const { makeSystemAudioHelper } = await loadHelper()
    const helper = makeSystemAudioHelper({ platform: 'linux', binPath: '/tmp/muesli-audiotap' })

    expect(helper.available()).toBe(false)
    expect(await helper.start(() => {})).toBeNull()
    expect(spawnMock).not.toHaveBeenCalled()
  })

  it('reports unavailable on macOS when the helper binary path is empty', async () => {
    const { makeSystemAudioHelper } = await loadHelper()
    const helper = makeSystemAudioHelper({ platform: 'darwin', binPath: '' })

    expect(helper.available()).toBe(false)
    expect(await helper.start(() => {})).toBeNull()
    expect(spawnMock).not.toHaveBeenCalled()
  })

  it('parses the META header and streams PCM', async () => {
    const { makeSystemAudioHelper } = await loadHelper()
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)
    const helper = makeSystemAudioHelper({
      platform: 'darwin',
      binPath: '/tmp/muesli-audiotap',
      spawnImpl: spawnMock,
    })

    const pcm: Uint8Array[] = []
    const started = helper.start((c) => pcm.push(c))
    child.stdout.write('META sr=48000 ch=2 fmt=f32le\n')

    await expect(started).resolves.toEqual({ sampleRate: 48000, channels: 2 })
    child.stdout.write(Buffer.from([1, 2, 3, 4]))
    expect(pcm.at(-1)).toEqual(new Uint8Array([1, 2, 3, 4]))
    await helper.stop()

    expect(spawnMock).toHaveBeenCalledWith('/tmp/muesli-audiotap', ['--start'], expect.objectContaining({
      stdio: ['ignore', 'pipe', 'pipe'],
    }))
    expect(child.kill).toHaveBeenCalledWith('SIGTERM')
  })

  it('handles header + PCM in one chunk', async () => {
    const { makeSystemAudioHelper } = await loadHelper()
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)
    const helper = makeSystemAudioHelper({
      platform: 'darwin',
      binPath: '/tmp/muesli-audiotap',
      spawnImpl: spawnMock,
    })

    const pcm: Uint8Array[] = []
    const started = helper.start((c) => pcm.push(c))
    child.stdout.write(Buffer.concat([Buffer.from('META sr=48000 ch=2 fmt=f32le\n'), Buffer.from([9, 9])]))

    await expect(started).resolves.toEqual({ sampleRate: 48000, channels: 2 })
    expect(pcm).toEqual([new Uint8Array([9, 9])])
    await helper.stop()
  })

  it("resolves null when the child emits an 'error' event", async () => {
    const { makeSystemAudioHelper } = await loadHelper()
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)
    const helper = makeSystemAudioHelper({
      platform: 'darwin',
      binPath: '/tmp/muesli-audiotap',
      spawnImpl: spawnMock,
    })

    const pcm: Uint8Array[] = []
    const started = helper.start((c) => pcm.push(c))
    child.emit('error', new Error('ENOENT'))

    await expect(started).resolves.toBeNull()
    expect(pcm).toEqual([])
    expect(child.kill).toHaveBeenCalledWith('SIGTERM')
    await helper.stop()
  })

  it('resolves null and kills the child when no header arrives before the start timeout', async () => {
    vi.useFakeTimers()
    const { makeSystemAudioHelper } = await loadHelper()
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)
    const helper = makeSystemAudioHelper({
      platform: 'darwin',
      binPath: '/tmp/muesli-audiotap',
      spawnImpl: spawnMock,
      startTimeoutMs: 25,
    })

    const started = helper.start(() => {})
    await vi.advanceTimersByTimeAsync(25)

    await expect(started).resolves.toBeNull()
    expect(child.kill).toHaveBeenCalledWith('SIGTERM')
    vi.useRealTimers()
  })

  it('resolves null when the process exits before a header', async () => {
    const { makeSystemAudioHelper } = await loadHelper()
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)
    const helper = makeSystemAudioHelper({
      platform: 'darwin',
      binPath: '/tmp/muesli-audiotap',
      spawnImpl: spawnMock,
    })

    const started = helper.start(() => {})
    child.emit('exit', 1, null)

    await expect(started).resolves.toBeNull()
    await helper.stop()
  })

  it('does not throw when exit fires after the header has already resolved', async () => {
    const { makeSystemAudioHelper } = await loadHelper()
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)
    const helper = makeSystemAudioHelper({
      platform: 'darwin',
      binPath: '/tmp/muesli-audiotap',
      spawnImpl: spawnMock,
    })

    const started = helper.start(() => {})
    child.stdout.write('META sr=48000 ch=2 fmt=f32le\n')

    await expect(started).resolves.toEqual({ sampleRate: 48000, channels: 2 })
    expect(() => child.emit('exit', 0, null)).not.toThrow()
    await helper.stop()
  })
})
