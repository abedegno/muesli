import { afterEach, expect, it } from 'vitest'
import { mkdtemp, rm, stat, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { LocaleSettings } from './localeSettings'
const roots: string[] = []
afterEach(async () => {
  await Promise.all(roots.splice(0).map((path) => rm(path, { recursive: true, force: true })))
})
it('keeps native language preferences private and scoped to the desktop profile', async () => {
  const root = await mkdtemp(join(tmpdir(), 'muesli-locale-'))
  roots.push(root)
  const locale = new LocaleSettings(root, () => 'de-DE')
  expect(locale.text('Open Muesli')).toBe('Muesli öffnen')
  locale.set('fr')
  expect(new LocaleSettings(root, () => 'en').text('Quit Muesli')).toBe('Quitter Muesli')
  expect((await stat(join(root, 'ui-language.json'))).mode & 0o777).toBe(0o600)
  expect(() => locale.set('unsupported')).toThrow()
  locale.set('system')
  expect(locale.text('New meeting')).toBe('Neues Meeting')
  await writeFile(join(root, 'ui-language.json'), 'malformed')
  expect(locale.text('New meeting')).toBe('Neues Meeting')
  expect(new LocaleSettings(root, () => 'unsupported').text('New meeting')).toBe('New meeting')
})
