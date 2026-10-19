import { stat } from 'node:fs/promises'
import { homedir } from 'node:os'
import { join } from 'node:path'
import type { LocalOllamaStatus } from '../shared/ipc'

interface OllamaSetupDeps {
  platform: NodeJS.Platform
  baseUrl: string
  isHosted(): boolean
  installedApp?: () => Promise<string | null>
  fetch?: typeof fetch
  openExternal(url: string): Promise<void>
  openPath(path: string): Promise<string>
  getOnboarded(): boolean
  setOnboarded(value: boolean): void
  restart(): void
}

async function findMacApp(): Promise<string | null> {
  for (const path of ['/Applications/Ollama.app', join(homedir(), 'Applications', 'Ollama.app')]) {
    try {
      if ((await stat(path)).isDirectory()) return path
    } catch {
      // Try the next supported installation location.
    }
  }
  return null
}

export function createOllamaSetup(deps: OllamaSetupDeps) {
  const fetchImpl = deps.fetch ?? globalThis.fetch.bind(globalThis)
  const installedApp = deps.installedApp ?? findMacApp
  let pending: Promise<LocalOllamaStatus> | null = null
  let restarting = false

  function requireLocal() {
    if (deps.isHosted())
      throw new Error('AI for your connected server is managed by its administrator.')
  }

  async function status(): Promise<LocalOllamaStatus> {
    requireLocal()
    if (pending) return pending
    pending = (async () => {
      const appInstalled = deps.platform === 'darwin' && (await installedApp()) !== null
      try {
        const response = await fetchImpl(
          new URL(`${deps.baseUrl.replace(/\/+$/, '')}/api/version`),
          {
            signal: AbortSignal.timeout(1500),
            redirect: 'error',
          }
        )
        // The version payload is not needed. Cancel the body so a probe does
        // not leave response streams open across successive checks.
        await response.body?.cancel()
        return { reachable: response.ok, appInstalled }
      } catch {
        return { reachable: false, appInstalled }
      }
    })()
    try {
      return await pending
    } finally {
      pending = null
    }
  }

  async function download() {
    requireLocal()
    await deps.openExternal(
      deps.platform === 'darwin'
        ? 'https://ollama.com/download/Ollama.dmg'
        : 'https://ollama.com/download'
    )
  }

  async function openApp() {
    requireLocal()
    if (deps.platform !== 'darwin') throw new Error('Open Ollama from your applications menu.')
    const path = await installedApp()
    requireLocal()
    if (!path) throw new Error('Install Ollama in Applications first, then try again.')
    const error = await deps.openPath(path)
    if (error) throw new Error(error)
  }

  async function finish() {
    requireLocal()
    if (restarting) return
    restarting = true
    try {
      if (!(await status()).reachable) throw new Error('Start Ollama before finishing AI setup.')
      requireLocal()
      const previous = deps.getOnboarded()
      deps.setOnboarded(true)
      try {
        deps.restart()
      } catch (error) {
        deps.setOnboarded(previous)
        throw error
      }
    } catch (error) {
      restarting = false
      throw error
    }
  }

  return { status, download, openApp, finish }
}
