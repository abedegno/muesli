import { _electron } from '@playwright/test'
import type { ElectronApplication } from '@playwright/test'
import { buildElectronLaunchOptions, expect, test } from '../fixtures/app'

test.setTimeout(120_000)
test('changes interface language live and remembers it after restarting the same profile', async ({
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
    await page.getByRole('link', { name: 'Settings', exact: true }).click()
    await page.getByLabel('Display language', { exact: true }).selectOption('fr')
    await expect(page.getByRole('heading', { name: 'Paramètres', exact: true })).toBeVisible()
    await page.getByLabel('Format régional', { exact: true }).selectOption('en-GB')
    await page.screenshot({ path: test.info().outputPath('settings-french.png'), fullPage: true })
    await app.close()
    app = await _electron.launch(options)
    page = await app.firstWindow()
    await expect(page.getByRole('link', { name: 'Toutes les notes', exact: true })).toBeVisible({
      timeout: 60_000,
    })
    await page.getByRole('link', { name: 'Paramètres', exact: true }).click()
    await expect(page.getByLabel('Langue d’affichage', { exact: true })).toHaveValue('fr')
    await expect(page.getByLabel('Format régional', { exact: true })).toHaveValue('en-GB')
    await page.getByLabel('Langue d’affichage', { exact: true }).selectOption('de')
    await expect(page.getByRole('heading', { name: 'Einstellungen', exact: true })).toBeVisible()
  } finally {
    if (app) await app.close()
  }
})
