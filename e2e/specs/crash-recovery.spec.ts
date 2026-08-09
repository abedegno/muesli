import { join, resolve } from 'node:path'
import { _electron } from '@playwright/test'
import type { ElectronApplication } from '@playwright/test'
import { expect, test } from '../fixtures/app'
import type { MuesliBridge } from '../../src/shared/ipc'

type MuesliWindow = Window & typeof globalThis & { muesli: MuesliBridge }

test.setTimeout(180_000)

test('a note is visible immediately after recovery from an abnormal exit', async ({
  electronApp,
  fakeTranscript,
  serverAddr,
  userDataDir,
}) => {
  test.fail(true, '#585: renderer races the embedded server and shows the connect screen')

  const page = await electronApp.firstWindow()
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible({ timeout: 60_000 })
  const title = 'Crash recovery keeps the cobalt lighthouse note'
  const noteId = await page.evaluate(
    async (noteTitle) => (await (window as MuesliWindow).muesli.createNote(noteTitle)).id,
    title
  )
  await expect
    .poll(
      () =>
        page.evaluate(async (id) => {
          const notes = await (window as MuesliWindow).muesli.listNotes()
          return notes.some((note) => note.id === id)
        }, noteId),
      { timeout: 30_000 }
    )
    .toBe(true)

  const appProcess = electronApp.process()
  appProcess.kill('SIGKILL')
  await expect
    .poll(() => appProcess.killed || appProcess.exitCode !== null, { timeout: 15_000 })
    .toBe(true)

  const pgBinaries = process.env.MUESLI_EMBEDDED_PG_BINARIES ?? ''
  const pgVector = process.env.MUESLI_EMBEDDED_PGVECTOR_DIR ?? ''
  if (pgBinaries === '' || pgVector === '') {
    throw new Error('Embedded Postgres environment disappeared before relaunch')
  }

  const binDir = resolve('e2e/.bin')
  let relaunchedApp: ElectronApplication | undefined
  try {
    relaunchedApp = await _electron.launch({
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
    const recoveredPage = await relaunchedApp.firstWindow()
    await expect(recoveredPage.getByText(title)).toBeVisible({ timeout: 45_000 })
    await expect(recoveredPage.getByText('First run (create the account)')).toBeHidden()
  } finally {
    await relaunchedApp?.close()
  }
})
