import { describe, expect, it, vi } from 'vitest'
import { createOllamaSetup } from './ollamaSetup'

function setup(overrides: Partial<Parameters<typeof createOllamaSetup>[0]> = {}) {
  const deps = {
    platform: 'darwin' as NodeJS.Platform,
    baseUrl: 'http://127.0.0.1:11434',
    isHosted: () => false,
    installedApp: vi.fn().mockResolvedValue('/Applications/Ollama.app'),
    fetch: vi.fn().mockResolvedValue(new Response(null, { status: 200 })),
    openExternal: vi.fn().mockResolvedValue(undefined),
    openPath: vi.fn().mockResolvedValue(''),
    getOnboarded: () => false,
    setOnboarded: vi.fn(),
    restart: vi.fn(),
    ...overrides,
  }
  return { deps, api: createOllamaSetup(deps) }
}

describe('local Ollama setup', () => {
  it('probes the live service without sending saved Muesli credentials or following redirects', async () => {
    const { api, deps } = setup()
    expect(await api.status()).toEqual({ reachable: true, appInstalled: true })
    const [url, init] = vi.mocked(deps.fetch).mock.calls[0] as unknown as [URL, RequestInit]
    expect(url.toString()).toBe('http://127.0.0.1:11434/api/version')
    expect(init.redirect).toBe('error')
    expect(init.signal).toBeInstanceOf(AbortSignal)
    expect(init.headers).toBeUndefined()
  })

  it('reports offline and non-success responses without treating an installed app as running', async () => {
    const { api, deps } = setup()
    vi.mocked(deps.fetch).mockResolvedValueOnce(new Response(null, { status: 503 }))
    expect(await api.status()).toEqual({ reachable: false, appInstalled: true })
    vi.mocked(deps.fetch).mockRejectedValueOnce(new Error('offline'))
    expect(await api.status()).toEqual({ reachable: false, appInstalled: true })
  })

  it('shares simultaneous probes and releases the in-flight slot when they complete', async () => {
    let resolve!: (response: Response) => void
    const { api, deps } = setup({
      fetch: vi.fn().mockReturnValue(
        new Promise<Response>((done) => {
          resolve = done
        })
      ),
    })
    const a = api.status()
    const b = api.status()
    await vi.waitFor(() => expect(deps.fetch).toHaveBeenCalledOnce())
    resolve(new Response(null, { status: 200 }))
    await Promise.all([a, b])
    await api.status()
    expect(deps.fetch).toHaveBeenCalledTimes(2)
  })

  it('aborts a stalled live probe so an unavailable service cannot block setup', async () => {
    let aborted = false
    const { api } = setup({
      fetch: vi.fn(
        (_url, init) =>
          new Promise<Response>((_resolve, reject) => {
            init?.signal?.addEventListener(
              'abort',
              () => {
                aborted = true
                reject(init.signal?.reason)
              },
              { once: true }
            )
          })
      ),
    })
    expect((await api.status()).reachable).toBe(false)
    expect(aborted).toBe(true)
  })

  it('does not restart if the connection changes to hosted while a probe is pending', async () => {
    let hosted = false
    let resolve!: (response: Response) => void
    const { api, deps } = setup({
      isHosted: () => hosted,
      fetch: vi.fn().mockReturnValue(
        new Promise<Response>((done) => {
          resolve = done
        })
      ),
    })
    const finishing = api.finish()
    await vi.waitFor(() => expect(deps.fetch).toHaveBeenCalledOnce())
    hosted = true
    resolve(new Response(null, { status: 200 }))
    await expect(finishing).rejects.toThrow(/connected server/i)
    expect(deps.setOnboarded).not.toHaveBeenCalled()
    expect(deps.restart).not.toHaveBeenCalled()
  })

  it('opens the official Mac installer and starts the installed app through the OS', async () => {
    const { api, deps } = setup()
    await api.download()
    expect(deps.openExternal).toHaveBeenCalledWith('https://ollama.com/download/Ollama.dmg')
    await api.openApp()
    expect(deps.openPath).toHaveBeenCalledWith('/Applications/Ollama.app')
  })

  it('reports installation and OS launch errors', async () => {
    const { api, deps } = setup({ installedApp: vi.fn().mockResolvedValue(null) })
    await expect(api.openApp()).rejects.toThrow(/install Ollama/i)
    expect(deps.openPath).not.toHaveBeenCalled()
    vi.mocked(deps.installedApp).mockResolvedValue('/Applications/Ollama.app')
    vi.mocked(deps.openPath).mockResolvedValue('Application could not be opened')
    await expect(api.openApp()).rejects.toThrow('Application could not be opened')
  })

  it('blocks all local setup operations for hosted connections', async () => {
    const { api, deps } = setup({ isHosted: () => true })
    for (const operation of [api.status, api.download, api.openApp, api.finish]) {
      await expect(operation()).rejects.toThrow(/connected server/i)
    }
    expect(deps.fetch).not.toHaveBeenCalled()
    expect(deps.openExternal).not.toHaveBeenCalled()
    expect(deps.openPath).not.toHaveBeenCalled()
    expect(deps.restart).not.toHaveBeenCalled()
  })

  it('only restarts after a live probe succeeds and completes onboarding first', async () => {
    const { api, deps } = setup()
    vi.mocked(deps.fetch).mockResolvedValueOnce(new Response(null, { status: 503 }))
    await expect(api.finish()).rejects.toThrow(/start Ollama/i)
    expect(deps.restart).not.toHaveBeenCalled()
    await api.finish()
    expect(deps.setOnboarded).toHaveBeenCalledWith(true)
    expect(vi.mocked(deps.setOnboarded).mock.invocationCallOrder[0]).toBeLessThan(
      vi.mocked(deps.restart).mock.invocationCallOrder[0]
    )
    await api.finish()
    expect(deps.restart).toHaveBeenCalledOnce()
  })

  it('restores onboarding if the OS cannot schedule a restart', async () => {
    const { api, deps } = setup({
      restart: vi.fn(() => {
        throw new Error('restart unavailable')
      }),
    })
    await expect(api.finish()).rejects.toThrow('restart unavailable')
    expect(vi.mocked(deps.setOnboarded).mock.calls).toEqual([[true], [false]])
  })
})
