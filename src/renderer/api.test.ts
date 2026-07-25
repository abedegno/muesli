// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest'

// Electron's `contextBridge.exposeInMainWorld` installs the bridge as a
// READ-ONLY, NON-CONFIGURABLE data property, and every function on it is
// likewise non-configurable. That matters: a `Proxy` whose `get` trap returns
// anything other than the target's own value for such a property violates a
// JavaScript proxy invariant and throws
//
//   TypeError: 'get' on proxy: property 'x' is a read-only and non-configurable
//   data property on the proxy target but the proxy did not return its actual value
//
// A plain object literal (what most tests stub `window.muesli` with) has
// configurable properties, so it does NOT reproduce this — which is exactly how
// a renderer-crashing regression shipped in desktop v0.1.10 with green tests.
// This helper reproduces the real shape.
function installContextBridgeLike(props: Record<string, unknown>): void {
  const bridge = {}
  for (const [key, value] of Object.entries(props)) {
    Object.defineProperty(bridge, key, {
      value,
      writable: false,
      configurable: false,
      enumerable: true,
    })
  }
  Object.defineProperty(window, 'muesli', {
    value: bridge,
    writable: false,
    configurable: true, // configurable so tests can reinstall between cases
    enumerable: true,
  })
}

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
