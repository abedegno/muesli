import { _electron, type ElectronApplication } from '@playwright/test'
import { buildElectronLaunchOptions, expect, test } from '../fixtures/app'
import { waitForMuesliConnection } from '../helpers/seed'

test.setTimeout(180_000)

test('retains alternative transcription services and live-off across real restarts', async ({
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
  let app: ElectronApplication | undefined
  try {
    app = await _electron.launch(options)
    let page = await app.firstWindow()
    await waitForMuesliConnection(page)
    const ids = await page.evaluate(async () => {
      const batch = await window.muesli.saveTranscriptionService({
        kind: 'transcriber',
        url: 'http://127.0.0.1:1/final',
        token: '',
      })
      const live = await window.muesli.saveTranscriptionService({
        kind: 'streaming-transcriber',
        url: 'http://127.0.0.1:1/live',
        token: '',
      })
      return { batch: batch.id, live: live.id }
    })
    await app.close()
    app = await _electron.launch(options)
    page = await app.firstWindow()
    await expect
      .poll(
        async () =>
          page.evaluate(async () => {
            try {
              return (await window.muesli.listPlugins())
                .filter((p) => p.enabled && p.is_default && p.kind !== 'agent')
                .map((p) => p.id)
                .sort()
            } catch {
              return []
            }
          }),
        { timeout: 60_000 }
      )
      .toEqual([ids.batch, ids.live].sort())
    await page.evaluate(() =>
      window.muesli.selectTranscriptionPlugin({ kind: 'streaming-transcriber', pluginId: null })
    )
    await app.close()
    app = await _electron.launch(options)
    page = await app.firstWindow()
    await expect
      .poll(
        async () =>
          page.evaluate(async () => {
            try {
              return (await window.muesli.listPlugins())
                .filter((p) => p.enabled && p.is_default && p.kind !== 'agent')
                .map((p) => p.id)
            } catch {
              return []
            }
          }),
        { timeout: 60_000 }
      )
      .toEqual([ids.batch])
  } finally {
    if (app) await app.close()
  }
})
