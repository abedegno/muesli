import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { _electron, expect, test as base } from '@playwright/test'
import type { ElectronApplication, Page } from '@playwright/test'

type AppFixtures = {
  electronApp: ElectronApplication
  page: Page
  userDataDir: string
  serverAddr: string
}

async function reservePort(): Promise<number> {
  const server = createServer()
  await new Promise<void>((resolveListen, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolveListen)
  })

  const address = server.address()
  if (address === null || typeof address === 'string') {
    server.close()
    throw new Error('Failed to reserve an E2E server port')
  }

  await new Promise<void>((resolveClose, reject) => {
    server.close((error) => (error ? reject(error) : resolveClose()))
  })
  return address.port
}

export const test = base.extend<AppFixtures>({
  userDataDir: async ({}, use) => {
    const dir = await mkdtemp(join(tmpdir(), 'muesli-e2e-'))
    try {
      await writeFile(
        join(dir, 'local-session.json'),
        JSON.stringify({ encrypted: false, manualServer: false, onboarded: true })
      )
      await use(dir)
    } finally {
      await rm(dir, { recursive: true, force: true })
    }
  },
  serverAddr: async ({}, use) => {
    const port = await reservePort()
    await use(`127.0.0.1:${port}`)
  },
  electronApp: async ({ serverAddr, userDataDir }, use) => {
    // resolveResourceEnv() (src/main/resourcePaths.ts) returns {} when the app is not
    // packaged, and it is the ONLY setter of MUESLI_MODE. config.Load reads MUESLI_MODE
    // rather than the --embedded argument, so an unpackaged launch starts a server that
    // demands an external DATABASE_URL. Supply it here: serverSupervisor spreads `...env`
    // AFTER resolveResourceEnv(), so these win.
    const pgBinaries = process.env.MUESLI_EMBEDDED_PG_BINARIES ?? ''
    const pgVector = process.env.MUESLI_EMBEDDED_PGVECTOR_DIR ?? ''
    if (pgBinaries === '' || pgVector === '') {
      // Fail here rather than letting the server fail far from the cause.
      throw new Error(
        'MUESLI_EMBEDDED_PG_BINARIES and MUESLI_EMBEDDED_PGVECTOR_DIR must be set. CI ' +
          'exports them in the embedded Postgres bundle step; locally, export them the ' +
          'same way the embedded-integration job does.'
      )
    }
    const binDir = resolve('e2e/.bin')
    const app = await _electron.launch({
      args: [
        'out/main/main.js',
        `--user-data-dir=${userDataDir}`,
        ...(process.env.CI ? ['--no-sandbox'] : []),
      ],
      env: {
        ...process.env,
        MUESLI_MODE: 'embedded',
        MUESLI_EMBEDDED_PG_BINARIES: pgBinaries,
        MUESLI_EMBEDDED_PGVECTOR_DIR: pgVector,
        MUESLI_ADDR: serverAddr,
        MUESLI_SERVER_BIN: join(binDir, 'muesli'),
        MUESLI_WHISPER_CPP_TRANSCRIBER_BIN: join(binDir, 'fakeplugin-transcriber'),
        MUESLI_OLLAMA_AGENT_BIN: join(binDir, 'fakeplugin-agent'),
        MUESLI_WHISPER_CPP_STREAMING_BIN: join(binDir, 'whisper-cpp-streaming'),
      },
    })
    try {
      await use(app)
    } finally {
      await app.close()
    }
  },
  page: async ({ electronApp }, use) => {
    await use(await electronApp.firstWindow())
  },
})

export { expect }
