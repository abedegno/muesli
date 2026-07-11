import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { TokenStore, type SafeStorageLike } from './tokenStore'

// Fake safeStorage: reversible "encryption" so we can assert ciphertext != plaintext.
const fakeSafe: SafeStorageLike = {
  isEncryptionAvailable: () => true,
  encryptString: (s: string) => Buffer.from(`enc:${s}`, 'utf8'),
  decryptString: (b: Buffer) => b.toString('utf8').replace(/^enc:/, ''),
}

describe('TokenStore', () => {
  let dir: string

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), 'muesli-tok-'))
  })
  afterEach(() => {
    rmSync(dir, { recursive: true, force: true })
  })

  it('returns null when nothing is saved', () => {
    const store = new TokenStore(dir, fakeSafe)
    expect(store.load()).toBeNull()
  })

  it('round-trips server URL + token, persisting ciphertext on disk', () => {
    const store = new TokenStore(dir, fakeSafe)
    store.save({ serverUrl: 'https://muesli.example', token: 'app-secret-123' })

    const reloaded = new TokenStore(dir, fakeSafe).load()
    expect(reloaded).toEqual({ serverUrl: 'https://muesli.example', token: 'app-secret-123' })

    // The raw file must not contain the plaintext token.
    const raw = store.rawFileContents()
    expect(raw).not.toContain('app-secret-123')
    // Sanity: only the encoded form is on disk. The fake encryptString returns
    // `enc:<token>`, which is then base64-encoded before being written, so the
    // base64 of the ciphertext is what appears in the file.
    expect(raw).toContain(Buffer.from('enc:app-secret-123', 'utf8').toString('base64'))
  })

  it('clear() removes the saved config', () => {
    const store = new TokenStore(dir, fakeSafe)
    store.save({ serverUrl: 'https://x', token: 't' })
    store.clear()
    expect(store.load()).toBeNull()
  })

  it('falls back to plaintext flag when encryption is unavailable', () => {
    const unavailable: SafeStorageLike = { ...fakeSafe, isEncryptionAvailable: () => false }
    const store = new TokenStore(dir, unavailable)
    store.save({ serverUrl: 'https://x', token: 'plain-token' })
    expect(new TokenStore(dir, unavailable).load()).toEqual({
      serverUrl: 'https://x',
      token: 'plain-token',
    })
  })
})
