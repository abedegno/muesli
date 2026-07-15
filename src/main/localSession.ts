import type { MuesliClient } from './muesliClient'
import type { SecretStore } from './secretStore'
import type { TokenStore } from './tokenStore'

export type LocalSessionResult = 'connected' | 'needs-setup' | 'manual' | 'skipped'

export interface LocalSessionDeps {
  embedded: boolean
  baseUrl: string
  tokenStore: Pick<TokenStore, 'load' | 'save'>
  secretStore: Pick<SecretStore, 'loadCreds' | 'saveCreds' | 'getManualServer'>
  makeClient: (baseUrl: string) => Pick<MuesliClient, 'setupNeeded' | 'setup' | 'login' | 'createToken'>
  generatePassword: () => string
  log?: (msg: string, err?: unknown) => void
}

export async function ensureLocalSession(deps: LocalSessionDeps): Promise<LocalSessionResult> {
  if (!deps.embedded) return 'skipped'
  if (deps.secretStore.getManualServer()) return 'manual'
  if (deps.tokenStore.load()) return 'connected'

  const client = deps.makeClient(deps.baseUrl)
  const needs = await client.setupNeeded()
  let creds = deps.secretStore.loadCreds()

  if (needs) {
    creds = {
      email: 'desktop@localhost',
      password: deps.generatePassword(),
    }
    await client.setup(creds.email, creds.password)
    deps.secretStore.saveCreds(creds)
  } else if (!creds) {
    return 'needs-setup'
  }

  const session = await client.login(creds.email, creds.password)
  const token = await client.createToken('muesli-desktop', session)
  deps.tokenStore.save({ serverUrl: deps.baseUrl, token })
  return 'connected'
}
