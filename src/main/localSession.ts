import type { MuesliClient } from './muesliClient'
import type { SecretStore } from './secretStore'
import type { TokenStore } from './tokenStore'

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
  if (!deps.embedded) return 'skipped'
  if (deps.secretStore.getManualServer()) return 'manual'
  const token = deps.tokenStore.load()
  const creds = deps.secretStore.loadCreds()

  // No embedded session HTTP call is safe until the server and its database
  // are queryable. This also protects a fresh install: setupNeeded must not be
  // the first request racing the Go server's listen socket.
  const isReady = deps.isReady ?? (async () => true)
  const sleep = deps.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)))
  const delays = deps.readinessDelaysMs ?? [0, 100, 200, 400, 800, 1000, 1000, 1000]
  for (const [index, delay] of delays.entries()) {
    if (delay > 0) await sleep(delay)
    if (await isReady().catch(() => false)) break
    if (index === delays.length - 1) return 'server-unreachable'
  }
  if (token) return 'connected'

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
    return 'needs-setup'
  }

  const session = await client.login(sessionCreds.email, sessionCreds.password)
  const appToken = await client.createToken('muesli-desktop', session)
  deps.tokenStore.save({ serverUrl: deps.baseUrl, token: appToken })
  return 'connected'
}
