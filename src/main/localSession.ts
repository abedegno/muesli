import type { MuesliClient } from './muesliClient'
import type { SecretStore } from './secretStore'
import type { TokenStore } from './tokenStore'

// Allow crash-recovery WAL replay ample time while remaining below the
// renderer/E2E visibility budget. The zero-delay probe keeps the normal path
// immediate; only an unavailable database consumes the trailing retries.
const DEFAULT_READINESS_DELAYS_MS = [0, 100, 200, 400, 800, ...Array<number>(32).fill(1000)]

export type LocalSessionResult = 'connected' | 'needs-setup' | 'server-unreachable' | 'manual' | 'skipped'

export interface LocalSessionDeps {
  embedded: boolean
  baseUrl: string
  tokenStore: Pick<TokenStore, 'load' | 'save'>
  secretStore: Pick<SecretStore, 'loadCreds' | 'saveCreds' | 'getManualServer'>
  makeClient: (baseUrl: string) => Pick<MuesliClient, 'setupNeeded' | 'setup' | 'login' | 'createToken'>
  generatePassword: () => string
  isReady?: () => Promise<boolean>
  sleep?: (ms: number) => Promise<void>
  readinessDelaysMs?: readonly number[]
  log?: (msg: string, err?: unknown) => void
}

export async function ensureLocalSession(deps: LocalSessionDeps): Promise<LocalSessionResult> {
  const finish = (result: LocalSessionResult): LocalSessionResult => {
    deps.log?.(`[muesli-debug] local session -> ${result}`)
    return result
  }
  if (!deps.embedded) return finish('skipped')
  if (deps.secretStore.getManualServer()) return finish('manual')
  const token = deps.tokenStore.load()
  const creds = deps.secretStore.loadCreds()

  // No embedded session HTTP call is safe until the server and its database
  // are queryable. This also protects a fresh install: setupNeeded must not be
  // the first request racing the Go server's listen socket.
  const isReady = deps.isReady ?? (async () => true)
  const sleep = deps.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)))
  const delays = deps.readinessDelaysMs ?? DEFAULT_READINESS_DELAYS_MS
  for (const [index, delay] of delays.entries()) {
    if (delay > 0) await sleep(delay)
    const ready = await isReady().catch((err) => {
      deps.log?.(`[muesli-debug] readiness ${index + 1}/${delays.length} threw`, err)
      return false
    })
    deps.log?.(`[muesli-debug] readiness ${index + 1}/${delays.length} -> ${ready}`)
    if (ready) break
    if (index === delays.length - 1) return finish('server-unreachable')
  }
  if (token) return finish('connected')

  const client = deps.makeClient(deps.baseUrl)
  const needs = await client.setupNeeded()
  let sessionCreds = creds

  if (needs) {
    sessionCreds = {
      email: 'desktop@localhost',
      password: deps.generatePassword(),
    }
    await client.setup(sessionCreds.email, sessionCreds.password)
    deps.secretStore.saveCreds(sessionCreds)
  } else if (!sessionCreds) {
    return finish('needs-setup')
  }

  const session = await client.login(sessionCreds.email, sessionCreds.password)
  const appToken = await client.createToken('muesli-desktop', session)
  deps.tokenStore.save({ serverUrl: deps.baseUrl, token: appToken })
  return finish('connected')
}
