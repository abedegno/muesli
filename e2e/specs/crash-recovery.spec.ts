import { _electron } from '@playwright/test'
import type { ElectronApplication } from '@playwright/test'
import { buildElectronLaunchOptions, expect, test } from '../fixtures/app'
import { seedNoteWithAudio } from '../helpers/seed'

test.setTimeout(180_000)

// This spec launches the app more than once per test (kill + recover), so it
// cannot use the shared `electronApp` fixture (one launch per test). It uses
// the same launch-config builder as that fixture instead, so it picks up
// packaged-app (MUESLI_E2E_APP_PATH) launches identically to every other spec.
async function launchApp({
  fakeTranscript,
  serverAddr,
  userDataDir,
}: {
  fakeTranscript: string
  serverAddr: string
  userDataDir: string
}): Promise<ElectronApplication> {
  return _electron.launch(buildElectronLaunchOptions({ fakeTranscript, serverAddr, userDataDir }))
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
    const title = 'Crash recovery keeps the cobalt lighthouse note'
    await seedNoteWithAudio(firstPage, { title })

    const appProcess = firstApp.process()
    appProcess.kill('SIGKILL')
    firstAppWasKilled = true
    // ChildProcess.killed only means the signal was SENT, so ORing it in here made
    // this resolve before the process had exited and left the relaunch racing the
    // dying instance -- scheduler-dependent, and a likely source of the
    // passes-on-branch/fails-on-main behaviour. Wait for actual exit: exitCode is
    // set when the process is reaped, and kill(pid, 0) throws once it is gone.
    await expect
      .poll(
        () => {
          if (appProcess.exitCode !== null) return true
          try {
            process.kill(appProcess.pid as number, 0)
            return false
          } catch {
            return true
          }
        },
        { timeout: 15_000 }
      )
      .toBe(true)

    recoveredApp = await launchApp(launchOptions)
    const recoveredPage = await recoveredApp.firstWindow()
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
