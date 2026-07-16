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
    expect(await helper.start()).toBeNull()
    expect(spawnMock).not.toHaveBeenCalled()
  })

  it('reports unavailable on macOS when the helper binary path is empty', async () => {
    const { makeSystemAudioHelper } = await loadHelper()
    const helper = makeSystemAudioHelper({ platform: 'darwin', binPath: '' })

    expect(helper.available()).toBe(false)
    expect(await helper.start()).toBeNull()
    expect(spawnMock).not.toHaveBeenCalled()
  })

  it('starts the helper, reads the device id, and stops it with SIGTERM', async () => {
    const { makeSystemAudioHelper } = await loadHelper()
    const child = createFakeChild()
    spawnMock.mockReturnValue(child)
    const helper = makeSystemAudioHelper({
      platform: 'darwin',
      binPath: '/tmp/muesli-audiotap',
      spawnImpl: spawnMock,
    })

    const started = helper.start()
    child.stdout.write('AudioTap:UID-123\n')

    await expect(started).resolves.toEqual({ deviceId: 'AudioTap:UID-123' })
    await helper.stop()

    expect(spawnMock).toHaveBeenCalledWith('/tmp/muesli-audiotap', ['--start'], expect.objectContaining({
      stdio: ['ignore', 'pipe', 'pipe'],
    }))
    expect(child.kill).toHaveBeenCalledWith('SIGTERM')
  })
})
