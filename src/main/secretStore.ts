import { chmodSync, existsSync, readFileSync, renameSync, rmSync, unlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

export interface SafeStorageLike {
  isEncryptionAvailable(): boolean
  encryptString(plaintext: string): Buffer
  decryptString(ciphertext: Buffer): string
}

export interface SecretCreds {
  email: string
  password: string
}

interface PersistedShape {
  encrypted: boolean
  manualServer: boolean
  onboarded: boolean
  creds?: string
}

const FILE = 'local-session.json'

function writeFileAtomic(path: string, data: string): void {
  const tempPath = `${path}.tmp-${process.pid}`
  try {
    writeFileSync(tempPath, data, { mode: 0o600 })
    renameSync(tempPath, path)
    chmodSync(path, 0o600)
  } catch (error) {
    try {
      unlinkSync(tempPath)
    } catch {
      // Best-effort cleanup only.
    }
    throw error
  }
}

function isSecretCreds(v: unknown): v is SecretCreds {
  return (
    typeof v === 'object' &&
    v !== null &&
    typeof (v as { email?: unknown }).email === 'string' &&
    typeof (v as { password?: unknown }).password === 'string'
  )
}

export class SecretStore {
  constructor(
    private readonly userDataDir: string,
    private readonly safe: SafeStorageLike,
  ) {}

  private get path(): string {
    return join(this.userDataDir, FILE)
  }

  private readPersisted(): PersistedShape | null {
    if (!existsSync(this.path)) return null
    try {
      const raw = JSON.parse(readFileSync(this.path, 'utf8')) as Partial<PersistedShape>
      return {
        encrypted: typeof raw.encrypted === 'boolean' ? raw.encrypted : this.safe.isEncryptionAvailable(),
        manualServer: raw.manualServer === true,
        onboarded: raw.onboarded === true,
        creds: typeof raw.creds === 'string' ? raw.creds : undefined,
      }
    } catch {
      return null
    }
  }

  private writePersisted(shape: PersistedShape): void {
    writeFileAtomic(this.path, JSON.stringify(shape))
  }

  loadCreds(): SecretCreds | null {
    const data = this.readPersisted()
    if (!data?.creds) return null

    const buf = Buffer.from(data.creds, 'base64')
    const json = data.encrypted ? this.safe.decryptString(buf) : buf.toString('utf8')
    try {
      const parsed = JSON.parse(json) as unknown
      return isSecretCreds(parsed) ? parsed : null
    } catch {
      return null
    }
  }

  saveCreds(creds: SecretCreds): void {
    const current = this.readPersisted()
    const encrypted = this.safe.isEncryptionAvailable()
    const payload = JSON.stringify(creds)
    const stored = encrypted
      ? this.safe.encryptString(payload).toString('base64')
      : Buffer.from(payload, 'utf8').toString('base64')

    this.writePersisted({
      encrypted,
      manualServer: current?.manualServer ?? false,
      onboarded: current?.onboarded ?? false,
      creds: stored,
    })
  }

  clearCreds(): void {
    const current = this.readPersisted()
    if (!current) return
    if (!current.manualServer) {
      rmSync(this.path, { force: true })
      return
    }

    this.writePersisted({
      encrypted: current.encrypted,
      manualServer: true,
      onboarded: current.onboarded,
    })
  }

  getManualServer(): boolean {
    return this.readPersisted()?.manualServer ?? false
  }

  setManualServer(manualServer: boolean): void {
    const current = this.readPersisted()
    const shape: PersistedShape = {
      encrypted: current?.encrypted ?? this.safe.isEncryptionAvailable(),
      manualServer,
      onboarded: current?.onboarded ?? false,
    }
    if (current?.creds) shape.creds = current.creds
    this.writePersisted(shape)
  }

  getOnboarded(): boolean {
    return this.readPersisted()?.onboarded ?? false
  }

  setOnboarded(onboarded: boolean): void {
    const current = this.readPersisted()
    const shape: PersistedShape = {
      encrypted: current?.encrypted ?? this.safe.isEncryptionAvailable(),
      manualServer: current?.manualServer ?? false,
      onboarded,
    }
    if (current?.creds) shape.creds = current.creds
    this.writePersisted(shape)
  }
}
