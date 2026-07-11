import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import type { ServerConfig } from '../shared/types'

// Subset of Electron's safeStorage we depend on (injectable for tests).
export interface SafeStorageLike {
  isEncryptionAvailable(): boolean
  encryptString(plaintext: string): Buffer
  decryptString(ciphertext: Buffer): string
}

interface PersistedShape {
  serverUrl: string
  encrypted: boolean
  // base64 of either ciphertext (encrypted) or utf8 token (fallback)
  token: string
}

const FILE = 'muesli-credentials.json'

// TokenStore persists the server URL + app token. The token is encrypted with
// the OS keychain via safeStorage when available; otherwise stored as a
// base64 plaintext with `encrypted:false` (e.g. headless Linux without a keyring).
export class TokenStore {
  constructor(
    private readonly userDataDir: string,
    private readonly safe: SafeStorageLike,
  ) {}

  private get path(): string {
    return join(this.userDataDir, FILE)
  }

  load(): ServerConfig | null {
    if (!existsSync(this.path)) return null
    const data = JSON.parse(readFileSync(this.path, 'utf8')) as PersistedShape
    const buf = Buffer.from(data.token, 'base64')
    const token = data.encrypted ? this.safe.decryptString(buf) : buf.toString('utf8')
    return { serverUrl: data.serverUrl, token }
  }

  save(config: ServerConfig): void {
    const canEncrypt = this.safe.isEncryptionAvailable()
    const tokenB64 = canEncrypt
      ? this.safe.encryptString(config.token).toString('base64')
      : Buffer.from(config.token, 'utf8').toString('base64')
    const shape: PersistedShape = {
      serverUrl: config.serverUrl,
      encrypted: canEncrypt,
      token: tokenB64,
    }
    writeFileSync(this.path, JSON.stringify(shape), { mode: 0o600 })
  }

  clear(): void {
    if (existsSync(this.path)) rmSync(this.path)
  }

  // Test/diagnostic helper: raw on-disk bytes (used to assert no plaintext leak).
  rawFileContents(): string {
    return existsSync(this.path) ? readFileSync(this.path, 'utf8') : ''
  }
}
