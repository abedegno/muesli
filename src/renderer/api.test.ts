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

  it('carries the HTTP status through to the BridgeError so callers can act on a 409', async () => {
    // The full production shape: muesliClient throws ApiError(409), ipcHandlers
    // re-encodes it as a `[409] ` message prefix (Electron drops custom error
    // properties), Electron wraps that in its own prefix. A caller that must
    // tell "the transcript changed, refetch" apart from "the request never
    // landed" needs the status to survive all three hops.
    installContextBridgeLike({
      postDiarizationReview: async () => {
        throw new Error(
          "Error invoking remote method 'muesli:postDiarizationReview': Error: [409] transcript changed, refetch and retry",
        )
      },
    })

    const { muesli, BridgeError } = await loadApi()

    await expect(muesli.postDiarizationReview('n1', { generation: 1 })).rejects.toSatisfy((err: unknown) => {
      expect(err).toBeInstanceOf(BridgeError)
      expect((err as InstanceType<typeof BridgeError>).status).toBe(409)
      expect((err as InstanceType<typeof BridgeError>).kind).toBe('ipc')
      expect((err as Error).message).toBe('transcript changed, refetch and retry')
      return true
    })
  })

  it('leaves status undefined when the failure never reached the server', async () => {
    installContextBridgeLike({
      createNote: async () => {
        throw new Error("Error invoking remote method 'muesli:createNote': Error: socket hang up")
      },
    })

    const { muesli } = await loadApi()

    await expect(muesli.createNote('x')).rejects.toSatisfy((err: unknown) => {
      expect((err as { status?: number }).status).toBeUndefined()
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
