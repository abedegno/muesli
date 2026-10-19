import { expect, test } from '../fixtures/app'

// Missing optional AI must not crowd the first-launch setup with a global warning.
test.use({ onboarded: false, ollamaUrl: 'http://127.0.0.1:1' })
test.setTimeout(120_000)

test('onboarding navigation fits at desktop and small window sizes without an optional AI warning', async ({
  page,
  electronApp,
}) => {
  await expect(page.getByRole('heading', { name: 'Let us set up the basics' })).toBeVisible({
    timeout: 60_000,
  })
  await expect(page.getByRole('button', { name: 'Dismiss', exact: true })).toBeHidden()
  const window = await electronApp.browserWindow(page)
  const next = page.getByRole('button', { name: 'Continue', exact: true })
  const back = page.getByRole('button', { name: 'Back', exact: true })

  for (const size of [
    { width: 1100, height: 730 },
    { width: 800, height: 500 },
    { width: 400, height: 500 },
  ]) {
    await window.evaluate((window, size) => window.setContentSize(size.width, size.height), size)
    await expect
      .poll(() => page.evaluate(() => ({ width: innerWidth, height: innerHeight })))
      .toEqual(size)
    await expect(page.getByLabel('Setup progress')).toBeInViewport({ ratio: 1 })
    await expect(next).toBeInViewport({ ratio: 1 })
    await next.click()
    await expect(page.getByRole('heading', { name: 'Microphone access' })).toBeVisible()
    await page.screenshot({ path: test.info().outputPath(`microphone-${size.width}.png`) })
    await expect(next).toBeInViewport({ ratio: 1 })
    await expect(back).toBeInViewport({ ratio: 1 })
    await next.click()
    await expect(page.getByRole('heading', { name: 'Transcription', exact: true })).toBeVisible()
    await expect(next).toBeInViewport({ ratio: 1 })
    await next.click()
    await expect(page.getByRole('heading', { name: 'Optional AI features' })).toBeVisible()
    const install = page.getByRole('button', {
      name: /^(Download Ollama(?: for Mac)?|Open Ollama)$/,
    })
    await install.scrollIntoViewIfNeeded()
    await expect(install).toBeInViewport({ ratio: 1 })
    await page.screenshot({ path: test.info().outputPath(`ai-setup-${size.width}.png`) })
    await expect(next).toBeInViewport({ ratio: 1 })
    await expect(back).toBeInViewport({ ratio: 1 })
    await expect(page.getByRole('button', { name: 'Skip', exact: true })).toBeInViewport({
      ratio: 1,
    })
    await back.click()
    await back.click()
    await back.click()
  }
})

test('a short window scrolls step content while keeping navigation visible', async ({
  page,
  electronApp,
}) => {
  await expect(page.getByRole('heading', { name: 'Let us set up the basics' })).toBeVisible({
    timeout: 60_000,
  })
  const window = await electronApp.browserWindow(page)
  await window.evaluate((window) => window.setContentSize(800, 500))
  const content = page.getByRole('region', { name: 'Setup step' })
  await expect(content).toBeVisible()
  await expect
    .poll(() => content.evaluate((element) => element.scrollHeight > element.clientHeight))
    .toBe(true)
  await content.hover()
  await page.mouse.wheel(0, 600)
  await expect.poll(() => content.evaluate((element) => element.scrollTop)).toBeGreaterThan(0)
  await expect(page.getByRole('button', { name: 'Continue', exact: true })).toBeInViewport({
    ratio: 1,
  })
  await expect(page.getByLabel('Setup progress')).toBeInViewport({ ratio: 1 })
})
