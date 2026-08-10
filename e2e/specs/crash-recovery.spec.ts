import { join, resolve } from 'node:path'
import { _electron } from '@playwright/test'
import { expect, test } from '../fixtures/app'
import { seedNoteWithAudio } from '../helpers/seed'

test.setTimeout(180_000)

test('recovers notes after an abnormal exit', async ({
  electronApp,
  serverAddr,
  userDataDir,
  fakeTranscript,
  page,
}) => {
  test.fail(
    true,
    '#585: relaunch can show the first-run/create-account screen even though notes remain intact'
  )

  const title = 'Crash recovery keeps the cobalt lighthouse note'
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible({ timeout: 60_000 })
  await seedNoteWithAudio(page, { title })

  const appProcess = electronApp.process()
  appProcess.kill('SIGKILL')
  await expect
    .poll(() => appProcess.killed || appProcess.exitCode !== null, { timeout: 15_000 })
    .toBe(true)

  const pgBinaries = process.env.MUESLI_EMBEDDED_PG_BINARIES ?? ''
  const pgVector = process.env.MUESLI_EMBEDDED_PGVECTOR_DIR ?? ''
  const binDir = resolve('e2e/.bin')
  const recoveredApp = await _electron.launch({
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

  try {
    const recoveredPage = await recoveredApp.firstWindow()
    await expect(recoveredPage.getByText(title)).toBeVisible({ timeout: 45_000 })
    await expect(recoveredPage.getByText('First run (create the account)')).toBeHidden()
  } finally {
    await recoveredApp.close()
  }
})
