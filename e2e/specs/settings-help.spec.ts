import { expect, test } from '../fixtures/app'

test('settings explains defaults and keeps advanced connections out of the main flow', async ({
  page,
}) => {
  await page.getByRole('link', { name: 'Settings' }).click()
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible()
  await expect(page.getByLabel('Final transcript', { exact: true })).toHaveValue(/.+/)
  await expect(page.getByLabel('Live transcript', { exact: true })).toHaveValue(/.+/)
  await expect(page.getByLabel('Access token')).toBeHidden()
  const help = page.getByRole('button', { name: 'Help: Live transcript' })
  await help.focus()
  await expect(page.getByRole('tooltip')).toContainText('during recording')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('tooltip')).toBeHidden()
  await help.click()
  await expect(page.getByRole('tooltip')).toContainText('during recording')
  await page.getByRole('heading', { name: 'Settings', exact: true }).click()
  await expect(page.getByRole('tooltip')).toBeHidden()
  await page.screenshot({ path: test.info().outputPath('settings.png'), fullPage: true })
  await page.keyboard.press('Tab')
  await page.evaluate(() => {
    document.documentElement.dir = 'rtl'
  })
  await help.focus()
  await expect(page.getByRole('tooltip')).toBeInViewport({ ratio: 1 })
  await page.screenshot({ path: test.info().outputPath('settings-rtl-layout.png'), fullPage: true })
})
