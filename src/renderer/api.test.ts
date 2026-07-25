// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { installContextBridgeLike } from './test-utils/bridge'

async function loadApi() {
  vi.resetModules()
  return import('./api')
}

describe('renderer bridge accessor', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  it('reads bridge members exposed the way contextBridge exposes them', async () => {
    installContextBridgeLike({
      platform: 'darwin',
      onEmbeddedStartupStatus: () => () => {},
      listNotes: async () => [{ id: 'n1' }],
    })

    const { muesli } = await loadApi()

    // The regression: merely *touching* the member threw before anything rendered.
    expect(() => muesli.onEmbeddedStartupStatus).not.toThrow()
    expect(typeof muesli.onEmbeddedStartupStatus).toBe('function')
    expect(muesli.platform).toBe('darwin')
    await expect(muesli.listNotes()).resolves.toEqual([{ id: 'n1' }])
  })

  it('still normalises a 401 rejection into an auth-invalidated BridgeError', async () => {
    installContextBridgeLike({
      createNote: async () => {
        throw new Error("Error invoking remote method 'muesli:createNote': [401] unauthorized")
      },
    })

    const { muesli, BridgeError } = await loadApi()

    await expect(muesli.createNote('x')).rejects.toSatisfy((err: unknown) => {
      expect(err).toBeInstanceOf(BridgeError)
      expect((err as InstanceType<typeof BridgeError>).kind).toBe('auth-invalidated')
      return true
    })
  })

  it('strips the IPC wrapper and error name from a non-auth error message', async () => {
    // Mirrors production shape: the main process rethrows muesliClient's
    // `ApiError`, which Electron wraps with the "Error invoking remote method"
    // prefix. Both layers should be stripped before the user sees it.
    installContextBridgeLike({
      createNote: async () => {
        throw new Error("Error invoking remote method 'muesli:createNote': ApiError: boom")
      },
    })

    const { muesli, BridgeError } = await loadApi()

    await expect(muesli.createNote('x')).rejects.toSatisfy((err: unknown) => {
      expect(err).toBeInstanceOf(BridgeError)
      expect((err as Error).message).toBe('boom')
      expect((err as InstanceType<typeof BridgeError>).kind).toBe('ipc')
      return true
    })
  })

  it('does not throw when the bridge is absent (non-Electron host)', async () => {
    Object.defineProperty(window, 'muesli', {
      value: undefined,
      writable: false,
      configurable: true,
      enumerable: true,
    })

    const { muesli } = await loadApi()

    expect(() => muesli.listNotes).not.toThrow()
  })
})
