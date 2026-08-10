import { join, resolve } from 'node:path'
import { _electron } from '@playwright/test'
import type { ElectronApplication } from '@playwright/test'
import { expect, test } from '../fixtures/app'
import { seedNoteWithAudio } from '../helpers/seed'

test.setTimeout(180_000)

async function launchApp({
  fakeTranscript,
  serverAddr,
  userDataDir,
}: {
  fakeTranscript: string
  serverAddr: string
  userDataDir: string
}): Promise<ElectronApplication> {
  const pgBinaries = process.env.MUESLI_EMBEDDED_PG_BINARIES ?? ''
  const pgVector = process.env.MUESLI_EMBEDDED_PGVECTOR_DIR ?? ''
  if (pgBinaries === '' || pgVector === '') {
    throw new Error(
      'MUESLI_EMBEDDED_PG_BINARIES and MUESLI_EMBEDDED_PGVECTOR_DIR must be set. CI ' +
        'exports them in the embedded Postgres bundle step; locally, export them the ' +
        'same way the embedded-integration job does.'
    )
  }

  const binDir = resolve('e2e/.bin')
  return _electron.launch({
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
      MUESLI_PUBLIC_URL: `http://${serverAddr}`,
      MUESLI_INTERNAL_URL: `http://${serverAddr}`,
      MUESLI_FAKE_TRANSCRIPT: fakeTranscript,
      MUESLI_SERVER_BIN: join(binDir, 'muesli'),
      MUESLI_WHISPER_CPP_TRANSCRIBER_BIN: join(binDir, 'fakeplugin-transcriber'),
      MUESLI_OLLAMA_AGENT_BIN: join(binDir, 'fakeplugin-agent'),
      MUESLI_WHISPER_CPP_STREAMING_BIN: join(binDir, 'whisper-cpp-streaming'),
    },
  })
}

function attachDiagnostics(
  label: 'first' | 'recovered',
  app: ElectronApplication,
  page: Awaited<ReturnType<ElectronApplication['firstWindow']>>
) {
  const child = app.process()
  child.stdout?.on('data', (data) => console.log(`[${label}-stdout]`, data.toString()))
  child.stderr?.on('data', (data) => console.error(`[${label}-stderr]`, data.toString()))
  page.on('console', (message) => console.log(`[${label}-console]`, message.text()))
  page.on('pageerror', (error) => console.error(`[${label}-pageerror]`, error.message))
}

test('recovers notes after an abnormal exit', async ({
  fakeTranscript,
  serverAddr,
  userDataDir,
}) => {
  const launchOptions = { fakeTranscript, serverAddr, userDataDir }
  let firstApp: ElectronApplication | undefined
  let firstAppWasKilled = false
  let recoveredApp: ElectronApplication | undefined

  try {
    firstApp = await launchApp(launchOptions)
    const firstPage = await firstApp.firstWindow()
    attachDiagnostics('first', firstApp, firstPage)
    const title = 'Crash recovery keeps the cobalt lighthouse note'
    await seedNoteWithAudio(firstPage, { title })

    const appProcess = firstApp.process()
    appProcess.kill('SIGKILL')
    firstAppWasKilled = true
    await expect
      .poll(() => appProcess.killed || appProcess.exitCode !== null, { timeout: 15_000 })
      .toBe(true)

    recoveredApp = await launchApp(launchOptions)
    const recoveredPage = await recoveredApp.firstWindow()
    attachDiagnostics('recovered', recoveredApp, recoveredPage)
    await expect(recoveredPage.getByText(title)).toBeVisible({ timeout: 45_000 })
    await expect(recoveredPage.getByText('First run (create the account)')).toBeHidden()
  } finally {
    if (recoveredApp !== undefined) {
      await recoveredApp.close()
    }
    if (firstApp !== undefined && !firstAppWasKilled) {
      await firstApp.close()
    }
  }
})
