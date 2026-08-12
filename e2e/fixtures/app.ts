import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { readdirSync } from 'node:fs'
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
  fakeTranscript: string
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

// Mirrors resolveBinary() in scripts/smoke-desktop.mjs: a packaged macOS .app
// carries its single launchable executable under Contents/MacOS/<entry>. The
// entry name is not hardcoded (electron-builder's productName can change) --
// read whatever the one entry there is.
function resolvePackagedBinary(appPath: string): string {
  const macos = join(appPath, 'Contents', 'MacOS')
  let entries: string[]
  try {
    entries = readdirSync(macos)
  } catch (err) {
    throw new Error(
      `MUESLI_E2E_APP_PATH=${appPath}: no Contents/MacOS directory (${err instanceof Error ? err.message : String(err)})`
    )
  }
  if (entries.length === 0)
    throw new Error(`MUESLI_E2E_APP_PATH=${appPath}: no executable in ${macos}`)
  return join(macos, entries[0])
}

type ElectronLaunchOptions = Parameters<typeof _electron.launch>[0]

// Builds the Electron launch config shared by the `electronApp` fixture below
// and by specs (crash-recovery.spec.ts) that need to launch the app more than
// once per test and so cannot use the fixture directly.
//
// Env-var gated packaged-app launch: when MUESLI_E2E_APP_PATH is unset,
// behavior is byte-for-byte identical to the dev-build launch this repo has
// always used (`out/main/main.js` via bare Electron) -- tier 1 in ci.yml is
// completely unaffected. When it is set to a built `.app` bundle (as wired in
// desktop-release.yml's nightly/tag-only packaged E2E step), this launches
// the packaged binary directly via `executablePath` instead.
export function buildElectronLaunchOptions({
  fakeTranscript,
  serverAddr,
  userDataDir,
}: {
  fakeTranscript: string
  serverAddr: string
  userDataDir: string
}): ElectronLaunchOptions {
  const binDir = resolve('e2e/.bin')

  // Deterministic substitutes for the real bundled transcriber/agent, in both
  // launch modes. serverSupervisor.ts spreads the env passed to `_electron.launch`
  // AFTER resolveResourceEnv() (src/main/resourcePaths.ts) when it spawns the
  // Go server, so these overrides win even against a packaged app's resolved
  // Resources-dir paths -- verified by reading startServerSupervisor() in
  // src/main/serverSupervisor.ts, not assumed.
  const fakeBinEnv = {
    MUESLI_WHISPER_CPP_TRANSCRIBER_BIN: join(binDir, 'fakeplugin-transcriber'),
    MUESLI_OLLAMA_AGENT_BIN: join(binDir, 'fakeplugin-agent'),
    MUESLI_WHISPER_CPP_STREAMING_BIN: join(binDir, 'whisper-cpp-streaming'),
  }

  const commonEnv = {
    ...process.env,
    MUESLI_ADDR: serverAddr,
    MUESLI_PUBLIC_URL: `http://${serverAddr}`,
    MUESLI_INTERNAL_URL: `http://${serverAddr}`,
    MUESLI_FAKE_TRANSCRIPT: fakeTranscript,
    ...fakeBinEnv,
  }

  const packagedAppPath = process.env.MUESLI_E2E_APP_PATH?.trim()
  if (packagedAppPath) {
    // Packaged mode: the app IS `isPackaged`, so resolveResourceEnv() already
    // sets MUESLI_MODE=embedded and MUESLI_EMBEDDED_PG_BINARIES /
    // MUESLI_EMBEDDED_PGVECTOR_DIR / MUESLI_SERVER_BIN from
    // process.resourcesPath -- scripts/assemble-desktop-resources.sh bundles
    // Postgres/pgvector into the Resources dir for darwin-arm64, so those
    // must NOT be overridden here; doing so would substitute a dev bundle
    // path that does not exist on the packaged-app runner.
    return {
      executablePath: resolvePackagedBinary(packagedAppPath),
      args: [`--user-data-dir=${userDataDir}`, ...(process.env.CI ? ['--no-sandbox'] : [])],
      env: commonEnv,
    }
  }

  // Dev-build mode (unchanged from before MUESLI_E2E_APP_PATH existed).
  // resolveResourceEnv() returns {} when the app is not packaged, and it is
  // the ONLY setter of MUESLI_MODE. config.Load reads MUESLI_MODE rather than
  // the --embedded argument, so an unpackaged launch starts a server that
  // demands an external DATABASE_URL. Supply it here: serverSupervisor
  // spreads `...env` AFTER resolveResourceEnv(), so these win.
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

  return {
    args: [
      'out/main/main.js',
      `--user-data-dir=${userDataDir}`,
      ...(process.env.CI ? ['--no-sandbox'] : []),
    ],
    env: {
      ...commonEnv,
      MUESLI_MODE: 'embedded',
      MUESLI_EMBEDDED_PG_BINARIES: pgBinaries,
      MUESLI_EMBEDDED_PGVECTOR_DIR: pgVector,
      MUESLI_SERVER_BIN: join(binDir, 'muesli'),
    },
  }
}

export const test = base.extend<AppFixtures>({
  fakeTranscript: ['hello from fakeplugin', { option: true }],
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
  electronApp: async ({ fakeTranscript, serverAddr, userDataDir }, use) => {
    const app = await _electron.launch(
      buildElectronLaunchOptions({ fakeTranscript, serverAddr, userDataDir })
    )
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
