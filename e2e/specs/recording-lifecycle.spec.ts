import { _electron } from '@playwright/test'
import { buildElectronLaunchOptions, expect, test } from '../fixtures/app'
import { waitForMuesliConnection } from '../helpers/seed'
import type { MuesliBridge } from '../../src/shared/ipc'

test.setTimeout(120_000)

test('a real renderer recording uploads on the actual embedded port and leaves recording state', async ({
  fakeTranscript,
  serverAddr,
  userDataDir,
}) => {
  const options = buildElectronLaunchOptions({
    fakeTranscript,
    serverAddr,
    userDataDir,
    ollamaUrl: 'http://127.0.0.1:1',
  })
  // Match the real launcher: it selects a listen address, without supplying URL overrides.
  delete options.env?.MUESLI_PUBLIC_URL
  delete options.env?.MUESLI_INTERNAL_URL
  options.args = [
    ...(options.args ?? []),
    '--use-fake-ui-for-media-stream',
    '--use-fake-device-for-media-stream',
  ]
  const app = await _electron.launch(options)
  app.on('console', (message) => {
    if (message.type() === 'error' && /main:|Error occurred in handler/.test(message.text()))
      console.error('Main process:', message.text())
  })
  try {
    // OS prompts and a system-audio tap cannot be automated. Exercise Chromium's
    // actual getUserMedia + MediaRecorder with its built-in fake audio device.
    const page = await app.firstWindow()
    page.on('pageerror', (error) => console.error('Renderer error:', error.message))
    await waitForMuesliConnection(page)
    await expect(page.getByRole('link', { name: 'All notes', exact: true })).toBeVisible({
      timeout: 60_000,
    })
    await app.evaluate(({ ipcMain }) => {
      ipcMain.removeHandler('muesli:micRequest')
      ipcMain.handle('muesli:micRequest', () => 'granted')
      ipcMain.removeHandler('muesli:systemAudioStart')
      ipcMain.handle('muesli:systemAudioStart', () => null)
    })
    await page
      .getByRole('complementary')
      .getByRole('button', { name: 'New meeting', exact: true })
      .click()
    await page.getByRole('button', { name: 'Record', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Stop', exact: true })).toBeVisible()
    await expect(page.getByText('0:01', { exact: true })).toBeVisible()
    await page
      .getByLabel('Note editor', { exact: true })
      .fill('My own notes survive stopping the recording.')
    const noteId = await page.evaluate(async () => {
      const bridge = (window as unknown as { muesli: MuesliBridge }).muesli
      const note = (await bridge.listNotes()).find((note) => note.status === 'recording')
      if (!note) throw new Error('Server never entered recording state')
      return note.id
    })
    const config = await page.evaluate(() =>
      (window as unknown as { muesli: MuesliBridge }).muesli.getConfig()
    )
    if (!config) throw new Error('Missing test server session')
    const uploadOrigin = await app.evaluate(
      async (_electron, { config, noteId }) => {
        const response = await fetch(`${config.serverUrl}/api/notes/${noteId}/audio-upload-url`, {
          method: 'POST',
          headers: { Authorization: `Bearer ${config.token}` },
        })
        const grant = (await response.json()) as { url: string }
        return new URL(grant.url).origin
      },
      { config, noteId }
    )
    expect(uploadOrigin).toBe(`http://${serverAddr}`)
    await page.getByRole('button', { name: 'Stop', exact: true }).click()
    await expect
      .poll(
        async () =>
          page.evaluate(async (id) => {
            const bridge = (window as unknown as { muesli: MuesliBridge }).muesli
            return (await bridge.getFull(id)).note.status
          }, noteId),
        { timeout: 45_000 }
      )
      .toBe('ready')
      .catch(async (error) => {
        console.log('Recorder outcome screen:', await page.locator('body').innerText())
        throw error
      })
    await expect(page.getByLabel('Note editor', { exact: true })).toContainText(
      'My own notes survive stopping the recording.'
    )
    const recordedNote = await page.evaluate(
      async (id) => (await (window as unknown as { muesli: MuesliBridge }).muesli.getFull(id)).note,
      noteId
    )
    expect(recordedNote.started_at).toBeTruthy()
    expect(recordedNote.ended_at).toBeTruthy()
    expect(Date.parse(recordedNote.ended_at!)).toBeGreaterThanOrEqual(
      Date.parse(recordedNote.started_at!)
    )
    const grant = await page.evaluate(
      async (id) => (window as unknown as { muesli: MuesliBridge }).muesli.getNoteAudioUrl(id),
      noteId
    )
    expect(new URL(grant!.url).origin).toBe(`http://${serverAddr}`)
    await expect(page.getByText(/Preparing upload…/)).toBeHidden()
    await page.getByRole('link', { name: 'All notes', exact: true }).click()
    await expect(page.getByText('Recording', { exact: true })).toBeHidden()
    // Leaving an active note must stop and save its owned capture, without
    // navigating the user back when background processing completes.
    await page
      .getByRole('complementary')
      .getByRole('button', { name: 'New meeting', exact: true })
      .click()
    await page.getByRole('button', { name: 'Record', exact: true }).click()
    await expect(page.getByText('0:01', { exact: true })).toBeVisible()
    const backgroundId = await page.evaluate(
      async () =>
        (await (window as unknown as { muesli: MuesliBridge }).muesli.listNotes()).find(
          (note) => note.status === 'recording'
        )?.id
    )
    expect(backgroundId).toBeTruthy()
    await page.getByRole('link', { name: 'All notes', exact: true }).click()
    await expect
      .poll(
        () =>
          page.evaluate(
            async (id) =>
              (window as unknown as { muesli: MuesliBridge }).muesli
                .getFull(id!)
                .then((full) => full.note.status),
            backgroundId
          ),
        { timeout: 45_000 }
      )
      .toBe('ready')
    await expect(page.getByRole('heading', { name: 'All notes', exact: true })).toBeVisible()
  } finally {
    await app.close()
  }
})

test('a failed upload remains recoverable after a real app restart', async ({
  fakeTranscript,
  serverAddr,
  userDataDir,
}) => {
  const { chmod } = await import('node:fs/promises')
  const { join } = await import('node:path')
  const { makeSilentWav } = await import('../helpers/wav')
  const options = buildElectronLaunchOptions({
    fakeTranscript,
    serverAddr,
    userDataDir,
    ollamaUrl: 'http://127.0.0.1:1',
  })
  options.args = [
    ...(options.args ?? []),
    '--use-fake-ui-for-media-stream',
    '--use-fake-device-for-media-stream',
  ]
  let app: Awaited<ReturnType<typeof _electron.launch>> | undefined
  const audioDirectory = join(userDataDir, 'embedded-server', 'storage', 'audio')
  try {
    app = await _electron.launch(options)
    let page = await app.firstWindow()
    await waitForMuesliConnection(page)
    await expect(page.getByRole('link', { name: 'All notes', exact: true })).toBeVisible({
      timeout: 60_000,
    })
    const noteId = await page.evaluate(() =>
      (window as unknown as { muesli: MuesliBridge }).muesli
        .createNote('Recording recovery regression')
        .then((note) => note.id)
    )
    await chmod(audioDirectory, 0o500)
    const audio = Array.from(makeSilentWav(1))
    const outcome = await page.evaluate(
      async ({ noteId, audio }) => {
        const bridge = (window as unknown as { muesli: MuesliBridge }).muesli
        try {
          await bridge.uploadAudio({
            noteId,
            audio: Uint8Array.from(audio).buffer,
            audioMimeType: 'audio/wav',
          })
          return 'unexpected success'
        } catch {
          return (await bridge.getRecordingRecovery(noteId))?.byteLength ?? 'missing backup'
        }
      },
      { noteId, audio }
    )
    expect(outcome).toBe(audio.length)
    await app.close()
    app = undefined
    await chmod(audioDirectory, 0o700)
    app = await _electron.launch(options)
    page = await app.firstWindow()
    await expect(page.getByRole('link', { name: 'All notes', exact: true })).toBeVisible({
      timeout: 60_000,
    })
    await page.getByRole('link', { name: 'All notes', exact: true }).click()
    await page.getByRole('button', { name: /Recording recovery regression/ }).click()
    await expect(page.getByRole('alert')).toContainText('saved on this device')
    await page.getByRole('button', { name: 'Retry upload', exact: true }).click()
    await expect
      .poll(
        () =>
          page.evaluate(
            async (id) =>
              (window as unknown as { muesli: MuesliBridge }).muesli
                .getFull(id)
                .then((full) => full.note.status),
            noteId
          ),
        { timeout: 45_000 }
      )
      .toBe('ready')
    expect(
      await page.evaluate(
        (id) => (window as unknown as { muesli: MuesliBridge }).muesli.getRecordingRecovery(id),
        noteId
      )
    ).toBeNull()
    await expect(page.getByRole('button', { name: 'Retry upload', exact: true })).toBeHidden()
  } finally {
    await chmod(audioDirectory, 0o700).catch(() => {})
    if (app) await app.close()
  }
})
