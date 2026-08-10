import { readFileSync } from 'node:fs'
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

test('recovers notes after an abnormal exit', async ({
  fakeTranscript,
  serverAddr,
  userDataDir,
}) => {
  test.fail(
    true,
    '#585: relaunch can show the first-run/create-account screen even though notes remain intact'
  )

  const launchOptions = { fakeTranscript, serverAddr, userDataDir }
  let firstApp: ElectronApplication | undefined
  let firstAppWasKilled = false
  let recoveredApp: ElectronApplication | undefined

  try {
    firstApp = await launchApp(launchOptions)
    const firstPage = await firstApp.firstWindow()
    // DIAGNOSTIC (#585) -- main has no readiness wait, so seeding races. Restored
    // here only so the probe can reach the recovery path at all.
    await expect
      .poll(
        () =>
          firstPage.evaluate(async () => {
            const cfg = await (
              window as unknown as { muesli: { getConfig(): Promise<unknown> } }
            ).muesli.getConfig()
            return cfg !== null
          }),
        { timeout: 90_000 }
      )
      .toBe(true)
    const title = 'Crash recovery keeps the cobalt lighthouse note'
    await seedNoteWithAudio(firstPage, { title })

    const preCrashStatus = await fetch(`http://${serverAddr}/api/setup/status`)
    console.log(
      `DIAG_PRE setup/status=${preCrashStatus.status} body=${await preCrashStatus.text()}`
    )
    // The token on disk is encrypted/base64; only TokenStore.load decodes it. Take
    // the REAL bearer token from getConfig() and prove it works before the crash,
    // so a post-crash 401 means something.
    const tokenBefore = await firstPage.evaluate(async () => {
      const cfg = await (
        window as unknown as { muesli: { getConfig(): Promise<{ token: string } | null> } }
      ).muesli.getConfig()
      return cfg === null ? '' : cfg.token
    })
    const control = await fetch(`http://${serverAddr}/api/notes`, {
      headers: { Authorization: `Bearer ${tokenBefore}` },
    })
    console.log(
      `DIAG_PRE control GET /api/notes -> ${control.status} (must be 200 or the probe is void)`
    )
    console.log(`DIAG_PRE dataDir=${join(userDataDir, 'embedded-server', 'postgres', 'data')}`)

    const appProcess = firstApp.process()
    appProcess.kill('SIGKILL')
    firstAppWasKilled = true
    await expect
      .poll(() => appProcess.killed || appProcess.exitCode !== null, { timeout: 15_000 })
      .toBe(true)

    // serverSupervisor opens server.log with flag 'w', so the relaunch destroys the
    // crashed instance's log. Keep a copy first.
    const logPath = join(userDataDir, 'logs', 'server.log')
    let firstLog = ''
    try {
      firstLog = readFileSync(logPath, 'utf8')
    } catch (err) {
      firstLog = `unreadable: ${String(err)}`
    }
    console.log(`DIAG_LOG1 first-launch server.log tail:\n${firstLog.slice(-1500)}`)

    recoveredApp = await launchApp(launchOptions)
    const recoveredPage = await recoveredApp.firstWindow()

    // DIAGNOSTIC (#585): does the DATABASE survive the crash? Asked from Node so
    // no renderer, no CORS, no auth. needs_setup=true => the user is gone and this
    // is data loss, not a session bug. Sampled over time to separate a transient
    // recovery window from a permanent state.
    for (let i = 0; i < 48; i++) {
      let line: string
      try {
        const r = await fetch(`http://${serverAddr}/api/setup/status`)
        line = `${r.status} ${(await r.text()).trim()}`
      } catch (err) {
        line = `THREW ${String(err)}`
      }
      let notesLine: string
      try {
        const n = await fetch(`http://${serverAddr}/api/notes`, {
          headers: { Authorization: `Bearer ${tokenBefore}` },
        })
        notesLine = `${n.status} ${(await n.text()).slice(0, 120).replace(/\s+/g, ' ')}`
      } catch (err) {
        notesLine = `THREW ${String(err)}`
      }
      console.log(`DIAG_REC t=${(i * 2.5).toFixed(1)}s setup/status=[${line}] notes=[${notesLine}]`)
      await new Promise((r) => setTimeout(r, 2_500))
    }

    let secondLog = ''
    try {
      secondLog = readFileSync(logPath, 'utf8')
    } catch (err) {
      secondLog = `unreadable: ${String(err)}`
    }
    console.log(`DIAG_LOG2 recovered server.log tail:\n${secondLog.slice(-3000)}`)

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
