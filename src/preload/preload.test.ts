import { beforeEach, describe, expect, it, vi } from 'vitest'
import { IPC } from '../shared/ipc'

const electron = vi.hoisted(() => {
  const listeners = new Map<string, Set<(...args: unknown[]) => void>>()

  const on = vi.fn((channel: string, listener: (...args: unknown[]) => void) => {
    const channelListeners = listeners.get(channel) ?? new Set<(...args: unknown[]) => void>()
    channelListeners.add(listener)
    listeners.set(channel, channelListeners)
  })

  const removeListener = vi.fn((channel: string, listener: (...args: unknown[]) => void) => {
    const channelListeners = listeners.get(channel)
    if (!channelListeners) {
      return
    }

    channelListeners.delete(listener)
    if (channelListeners.size === 0) {
      listeners.delete(channel)
    }
  })

  const invoke = vi.fn<(channel: string, ...args: unknown[]) => Promise<unknown>>(
    async () => undefined,
  )
  const exposeInMainWorld = vi.fn()

  const emit = (channel: string, ...args: unknown[]) => {
    for (const listener of listeners.get(channel) ?? []) {
      listener(...args)
    }
  }

  const listenerCount = (channel: string) => listeners.get(channel)?.size ?? 0

  const reset = () => {
    listeners.clear()
    on.mockClear()
    removeListener.mockClear()
    invoke.mockClear()
    exposeInMainWorld.mockClear()
  }

  return { on, removeListener, invoke, exposeInMainWorld, emit, listenerCount, reset }
})

vi.mock('electron', () => ({
  contextBridge: {
    exposeInMainWorld: electron.exposeInMainWorld,
  },
  ipcRenderer: {
    on: electron.on,
    removeListener: electron.removeListener,
    invoke: electron.invoke,
  },
}))

const eventMembers = {
  onAuthInvalidated: IPC.authInvalidated,
  onEmbeddedStartupStatus: IPC.embeddedStartupStatus,
  onMeetingDetectionPromptShow: IPC.meetingDetectionPromptShow,
  onMeetingDetectionPromptClear: IPC.meetingDetectionPromptClear,
  onMeetingDetectionAutoRecord: IPC.meetingDetectionAutoRecord,
  onNoteStreamEvent: IPC.noteStreamEvent,
  onSystemAudioPcm: IPC.systemAudioPcm,
  onUploadProgress: IPC.uploadProgress,
} as const

const eventChannels = new Set<string>(Object.values(eventMembers))
const invokeMemberNames = Object.entries(IPC)
  .filter(([, channel]) => !eventChannels.has(channel))
  .map(([name]) => name)

function loadPreload() {
  vi.resetModules()
  return import('./preload')
}

function expectBridgeSurface(bridge: Record<string, unknown>) {
  const expectedKeys = [
    'platform',
    ...Object.keys(eventMembers),
    ...invokeMemberNames,
  ].sort()

  expect(Object.keys(bridge).sort()).toEqual(expectedKeys)
  expect(bridge.platform).toBe(process.platform)

  for (const key of Object.keys(eventMembers)) {
    expect(typeof bridge[key]).toBe('function')
  }

  for (const key of invokeMemberNames) {
    expect(typeof bridge[key]).toBe('function')
  }
}

describe('preload bridge', () => {
  beforeEach(() => {
    electron.reset()
  })

  it('exposes the expected bridge surface in the main world exactly once', async () => {
    await loadPreload()

    expect(electron.exposeInMainWorld).toHaveBeenCalledTimes(1)
    expect(electron.exposeInMainWorld).toHaveBeenCalledWith(
      'muesli',
      expect.any(Object),
    )

    const [, bridge] = electron.exposeInMainWorld.mock.calls[0]
    expectBridgeSurface(bridge as Record<string, unknown>)
  })

  it.each(Object.entries(eventMembers))(
    '%s subscribes, forwards payloads unchanged, and unsubscribes the exact listener',
    async (memberName, channel) => {
      await loadPreload()
      const [, bridge] = electron.exposeInMainWorld.mock.calls[0]
      const preloadBridge = bridge as Record<
        string,
        (listener: (payload: unknown) => void) => () => void
      >
      const callback = vi.fn<(payload: unknown) => void>()
      const payloadByCycle = [{ memberName, cycle: 1 }, { memberName, cycle: 2 }]

      for (const payload of payloadByCycle) {
        electron.on.mockClear()
        electron.removeListener.mockClear()

        const unsubscribe = preloadBridge[memberName](callback)

        expect(electron.on).toHaveBeenCalledTimes(1)
        expect(electron.on).toHaveBeenCalledWith(channel, expect.any(Function))
        expect(electron.listenerCount(channel)).toBe(1)

        const wrappedListener = electron.on.mock.calls[0][1] as (...args: unknown[]) => void
        electron.emit(channel, { ignored: true }, payload)
        expect(callback).toHaveBeenCalledTimes(payloadByCycle.indexOf(payload) + 1)
        expect(callback).toHaveBeenLastCalledWith(payload)

        unsubscribe()

        expect(electron.removeListener).toHaveBeenCalledTimes(1)
        expect(electron.removeListener).toHaveBeenCalledWith(channel, wrappedListener)
        expect(electron.listenerCount(channel)).toBe(0)

        electron.emit(channel, { ignored: true }, { memberName, cycle: 99 })
        expect(callback).toHaveBeenCalledTimes(payloadByCycle.indexOf(payload) + 1)
      }
    },
  )

  it.each(invokeMemberNames)('%s forwards arguments and propagates invoke resolution and rejection', async (memberName) => {
    await loadPreload()
    const [, bridge] = electron.exposeInMainWorld.mock.calls[0]
    const preloadBridge = bridge as Record<string, (...args: unknown[]) => Promise<unknown>>
    const fn = preloadBridge[memberName]
    const arity = fn.length
    const args = Array.from({ length: arity }, (_, index) => ({ memberName, index }))
    const channel = IPC[memberName as keyof typeof IPC]

    const resolved = { memberName, kind: 'resolved' }
    electron.invoke.mockClear()
    electron.invoke.mockResolvedValueOnce(resolved)

    await expect(fn(...args)).resolves.toBe(resolved)
    expect(electron.invoke).toHaveBeenCalledTimes(1)
    expect(electron.invoke).toHaveBeenCalledWith(channel, ...args)

    const rejection = new Error(`invoke rejected for ${memberName}`)
    electron.invoke.mockClear()
    electron.invoke.mockRejectedValueOnce(rejection)

    await expect(fn(...args)).rejects.toBe(rejection)
    expect(electron.invoke).toHaveBeenCalledTimes(1)
    expect(electron.invoke).toHaveBeenCalledWith(channel, ...args)
  })
})
