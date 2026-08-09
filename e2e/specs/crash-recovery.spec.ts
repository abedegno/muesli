import { execFileSync } from 'node:child_process'
import { join, resolve } from 'node:path'
import { _electron } from '@playwright/test'
import type { ElectronApplication } from '@playwright/test'
import type { MuesliBridge } from '../../src/shared/ipc'
import { expect, test } from '../fixtures/app'

type MuesliWindow = Window & typeof globalThis & { muesli: MuesliBridge }

function collectDescendantPids(parentPid: number): number[] {
  const descendants: number[] = []
  const pending = [parentPid]

  while (pending.length > 0) {
    const pid = pending.pop()
    if (pid === undefined) continue

    try {
      const output = execFileSync('pgrep', ['-P', String(pid)], {
        encoding: 'utf8',
        stdio: ['ignore', 'pipe', 'ignore'],
      })
      const children = output
        .split('\n')
        .map((value) => Number.parseInt(value, 10))
        .filter(Number.isFinite)
      descendants.push(...children)
      pending.push(...children)
    } catch {
      // pgrep exits with status 1 when the process has no children or has already exited.
    }
  }

  return descendants
}

function killProcesses(pids: number[]): void {
  for (const pid of pids.reverse()) {
    try {
      process.kill(pid, 'SIGKILL')
    } catch {
      // A process may exit on its own after its parent is killed.
    }
  }
}

test.setTimeout(120_000)
test.fail(true, '#585: renderer races the embedded server and shows the connect screen')

test('recovers the existing session after an abnormal exit', async ({
  electronApp,
  fakeTranscript,
  serverAddr,
  userDataDir,
}) => {
  const title = 'Crash recovery sentinel — amber lighthouse'
  const page = await electronApp.firstWindow()
  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible({ timeout: 60_000 })
  await page.evaluate(async (noteTitle) => {
    await (window as MuesliWindow).muesli.createNote(noteTitle)
  }, title)
  await page.getByRole('link', { name: 'All notes' }).click()
  await expect(page.getByText(title)).toBeVisible()

  const originalProcess = electronApp.process()
  const originalPid = originalProcess.pid
  const descendantPids = originalPid === undefined ? [] : collectDescendantPids(originalPid)
  originalProcess.kill('SIGKILL')
  killProcesses(descendantPids)
  await expect
    .poll(() => originalProcess.killed || originalProcess.exitCode !== null, { timeout: 10_000 })
    .toBe(true)

  const pgBinaries = process.env.MUESLI_EMBEDDED_PG_BINARIES ?? ''
  const pgVector = process.env.MUESLI_EMBEDDED_PGVECTOR_DIR ?? ''
  if (pgBinaries === '' || pgVector === '') {
    throw new Error('Embedded Postgres paths disappeared before Electron relaunch')
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

    const relaunchedPage = await relaunchedApp.firstWindow()
    await expect(relaunchedPage.getByText(title)).toBeVisible({ timeout: 45_000 })
    await expect(relaunchedPage.getByText('First run (create the account)')).toBeHidden()
  } finally {
    await relaunchedApp?.close()
  }
})
