import { existsSync, readFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { _electron } from '@playwright/test'
import type { ElectronApplication } from '@playwright/test'
import { closeElectronApp, expect, test } from '../fixtures/app'
import { seedNoteWithAudio, waitForMuesliConnection } from '../helpers/seed'

test.setTimeout(180_000)

// TEMPORARY DIAGNOSTIC (#585) -- NOT FOR MERGE. Types for the in-page probe below.
type ProbeAttempt = { attempt: number; readyz: string; notes: string }
type MuesliProbeBridge = {
  getReadyz(): Promise<unknown>
  listNotes(): Promise<{ title: string }[]>
}

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
  const launchOptions = { fakeTranscript, serverAddr, userDataDir }
  let firstApp: ElectronApplication | undefined
  let firstAppWasKilled = false
  let recoveredApp: ElectronApplication | undefined

  try {
    firstApp = await launchApp(launchOptions)
    const firstPage = await firstApp.firstWindow()
    await waitForMuesliConnection(firstPage)
    const title = 'Crash recovery keeps the cobalt lighthouse note'
    await seedNoteWithAudio(firstPage, { title })

    // TEMPORARY DIAGNOSTIC (#585) -- capture the token before the crash clears it.
    const tokenBeforeCrash =
      (
        JSON.parse(readFileSync(join(userDataDir, 'muesli-credentials.json'), 'utf8')) as {
          token?: string
        }
      ).token ?? ''
    console.log(`DIAG0 token before crash len=${tokenBeforeCrash.length}`)

    const appProcess = firstApp.process()
    appProcess.kill('SIGKILL')
    firstAppWasKilled = true
    await expect
      .poll(() => appProcess.killed || appProcess.exitCode !== null, { timeout: 15_000 })
      .toBe(true)

    recoveredApp = await launchApp(launchOptions)
    const recoveredPage = await recoveredApp.firstWindow()
    await waitForMuesliConnection(recoveredPage)

    // TEMPORARY DIAGNOSTIC (#585) part 2 -- NOT FOR MERGE.
    // Asked from NODE, not the renderer: no CORS, and it can read the token file
    // directly. Separates "the DB lost the user" from "the DB kept the user but
    // the token row is gone".
    {
      const credsPath = join(userDataDir, 'muesli-credentials.json')
      const rawCreds = existsSync(credsPath) ? readFileSync(credsPath, 'utf8') : '(absent)'
      const savedToken = tokenBeforeCrash
      const status = await fetch(`http://${serverAddr}/api/setup/status`)
      console.log(`DIAG2 setup/status=${status.status} body=${await status.text()}`)
      const notes = await fetch(`http://${serverAddr}/api/notes`, {
        headers: { Authorization: `Bearer ${savedToken}` },
      })
      console.log(
        `DIAG2 GET /api/notes with saved token -> ${notes.status} body=${(await notes.text()).slice(0, 200)}`
      )
      console.log(`DIAG2 token file present=${rawCreds.length > 0} tokenLen=${savedToken.length}`)
    }

    // TEMPORARY DIAGNOSTIC (#585) -- NOT FOR MERGE.
    // Decides whether the recovered notes load REJECTS (server not ready, and
    // AppLayout never retries) or SUCCEEDS-EMPTY (a data/identity problem).
    // Both render an identical empty list, so no amount of DOM inspection can
    // separate them; only the call's own outcome can.
    const probe = await recoveredPage.evaluate(async () => {
      const bridge = (window as unknown as { muesli: MuesliProbeBridge }).muesli
      const attempts: ProbeAttempt[] = []
      for (let i = 0; i < 8; i++) {
        let readyz: string
        try {
          readyz = JSON.stringify(await bridge.getReadyz())
        } catch (err) {
          readyz = `THREW: ${String(err)}`
        }
        let notes: string
        try {
          const list = await bridge.listNotes()
          notes = `OK count=${list.length} titles=${JSON.stringify(list.map((n) => n.title))}`
        } catch (err) {
          notes = `REJECTED: ${String(err)}`
        }
        attempts.push({ attempt: i, readyz, notes })
        await new Promise((r) => setTimeout(r, 2_500))
      }
      return attempts
    })
    for (const a of probe) {
      console.log(`DIAG attempt=${a.attempt} readyz=${a.readyz} listNotes=${a.notes}`)
    }

    await expect(recoveredPage.getByText(title)).toBeVisible({ timeout: 90_000 })
    await expect(recoveredPage.getByText('First run (create the account)')).toBeHidden()
  } finally {
    if (recoveredApp !== undefined) {
      await closeElectronApp(recoveredApp)
    }
    if (firstApp !== undefined && !firstAppWasKilled) {
      await closeElectronApp(firstApp)
    }
  }
})
