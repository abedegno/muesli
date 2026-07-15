import { existsSync, statSync, mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { SecretStore, type SafeStorageLike } from './secretStore'

const fakeSafe: SafeStorageLike = {
  isEncryptionAvailable: () => true,
  encryptString: (s) => Buffer.from(`enc:${s}`, 'utf8'),
  decryptString: (b) => b.toString('utf8').replace(/^enc:/, ''),
}

describe('SecretStore', () => {
  let dir: string

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), 'muesli-secret-'))
  })

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true })
  })

  it('round-trips credentials through the encrypted file', () => {
    const store = new SecretStore(dir, fakeSafe)
    store.saveCreds({ email: 'desktop@localhost', password: 'secret-pw' })

    const reloaded = new SecretStore(dir, fakeSafe)
    expect(reloaded.loadCreds()).toEqual({ email: 'desktop@localhost', password: 'secret-pw' })

    const path = join(dir, 'local-session.json')
    expect(existsSync(path)).toBe(true)
    expect(statSync(path).mode & 0o777).toBe(0o600)
  })

  it('clears stored credentials', () => {
    const store = new SecretStore(dir, fakeSafe)
    store.saveCreds({ email: 'desktop@localhost', password: 'secret-pw' })
    expect(store.loadCreds()).toEqual({ email: 'desktop@localhost', password: 'secret-pw' })

    store.clearCreds()
    expect(store.loadCreds()).toBeNull()
  })

  it('persists the manualServer flag across instances and toggles it', () => {
    const store = new SecretStore(dir, fakeSafe)
    expect(store.getManualServer()).toBe(false)

    store.setManualServer(true)
    expect(new SecretStore(dir, fakeSafe).getManualServer()).toBe(true)

    const again = new SecretStore(dir, fakeSafe)
    again.setManualServer(false)
    expect(new SecretStore(dir, fakeSafe).getManualServer()).toBe(false)
  })

  it('persists the onboarded flag across instances and toggles it', () => {
    const store = new SecretStore(dir, fakeSafe)
    expect(store.getOnboarded()).toBe(false)

    store.setOnboarded(true)
    expect(new SecretStore(dir, fakeSafe).getOnboarded()).toBe(true)

    const again = new SecretStore(dir, fakeSafe)
    again.setOnboarded(false)
    expect(new SecretStore(dir, fakeSafe).getOnboarded()).toBe(false)
  })
})
