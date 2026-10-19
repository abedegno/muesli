import { afterEach, expect, it } from 'vitest'
import { mkdtemp, rm } from 'node:fs/promises'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { FakeServer } from '../../test/fakeServer'
import { SecretStore } from './secretStore'
import { TokenStore } from './tokenStore'
import { createHandlers } from './ipcHandlers'

let directory: string | null = null
afterEach(async () => {
  if (directory) await rm(directory, { recursive: true, force: true })
})
const safe = {
  isEncryptionAvailable: () => true,
  encryptString: (text: string) => Buffer.from(text),
  decryptString: (bytes: Buffer) => bytes.toString(),
}
it('returns to the same local account and notes after connecting to a team server', async () => {
  directory = await mkdtemp(join(tmpdir(), 'muesli-server-switch-'))
  const local = new FakeServer()
  const team = new FakeServer()
  team.seedUser('team@example.com', 'team-password')
  const secrets = new SecretStore(directory, safe)
  const handlers = createHandlers({
    tokenStore: new TokenStore(directory, safe),
    secretStore: secrets,
    embedded: true,
    embeddedBaseUrl: 'http://127.0.0.1:8080',
    generatePassword: () => 'local-generated-password',
    onProgress: () => {},
    fetch: (input, options) =>
      new URL(String(input)).hostname === 'team.example.com'
        ? team.fetch(input, options)
        : local.fetch(input, options),
  })
  expect(await handlers.getConfig()).not.toBeNull()
  const note = await handlers.createNote('Keep my local notes')
  const original = secrets.loadCreds()
  await handlers.disconnect()
  await handlers.connect({
    serverUrl: 'https://team.example.com',
    email: 'team@example.com',
    password: 'team-password',
    isFirstRun: false,
  })
  expect(await handlers.getManualServer()).toBe(true)
  expect(await handlers.listNotes()).toHaveLength(0)
  await handlers.resetToBuiltIn()
  expect(secrets.loadCreds()).toEqual(original)
  expect(await handlers.getConfig()).toMatchObject({
    serverUrl: 'http://127.0.0.1:8080',
    manualServer: false,
  })
  expect((await handlers.listNotes()).map((note) => note.id)).toContain(note.id)
})

it('does not send a delayed editor save to a different connected server', async () => {
  directory = await mkdtemp(join(tmpdir(), 'muesli-editor-scope-'))
  const local = new FakeServer(),
    team = new FakeServer()
  team.seedUser('team@example.com', 'team-password')
  const handlers = createHandlers({
    tokenStore: new TokenStore(directory, safe),
    secretStore: new SecretStore(directory, safe),
    embedded: true,
    embeddedBaseUrl: 'http://127.0.0.1:8080',
    generatePassword: () => 'local-password',
    onProgress: () => {},
    fetch: (input, options) =>
      new URL(String(input)).hostname === 'team.example.com'
        ? team.fetch(input, options)
        : local.fetch(input, options),
  })
  await handlers.getConfig()
  const localNote = await handlers.createNote('Local private notes')
  const snapshot = await handlers.getFull(localNote.id)
  await handlers.connect({
    serverUrl: 'https://team.example.com',
    email: 'team@example.com',
    password: 'team-password',
    isFirstRun: false,
  })
  const teamNote = await handlers.createNote('A different note')
  expect(teamNote.id).toBe(localNote.id) // two independent servers really can use the same identifier
  await expect(
    handlers.updateBody(
      localNote.id,
      'Private local draft',
      snapshot.connectionId ?? 'old-connection'
    )
  ).rejects.toThrow(/connection changed/)
  expect(team.lastBodyPut).toBeUndefined()
})
