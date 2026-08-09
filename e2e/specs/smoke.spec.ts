import { expect, test } from '../fixtures/app'

test('boots into the notes UI without page errors', async ({ page }) => {
  const pageErrors: Error[] = []
  page.on('pageerror', (error) => pageErrors.push(error))

  await expect(page.getByRole('link', { name: 'All notes' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'New meeting' }).first()).toBeVisible()
  expect(pageErrors).toEqual([])
})
