import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

function directives(policy: string): Map<string, string[]> {
  return new Map(
    policy
      .split(';')
      .map((directive) => directive.trim().split(/\s+/))
      .filter(([name]) => name)
      .map(([name, ...sources]) => [name, sources]),
  )
}

function expectRestrictiveRendererCsp(policy: string) {
  const parsed = directives(policy)

  expect(parsed.get('default-src')).toEqual(["'self'"])
  expect(parsed.get('media-src')).toEqual(["'self'", 'http://127.0.0.1:*', 'http://localhost:*'])
  // issue #767: the local-iOS-pairing QR code is rendered as a data: URI
  // <img> (QRCode.toDataURL in SettingsScreen.tsx). img-src falls back to
  // default-src when unset, but 'self' only covers same-origin fetches --
  // it does not cover the data: scheme, so without an explicit img-src
  // allowing data:, Electron's renderer silently refuses to load the QR
  // image. Must stay exactly these two sources, never widen to any other
  // scheme (e.g. http:/https: image sources).
  expect(parsed.get('img-src')).toEqual(["'self'", 'data:'])
}

describe('renderer Content Security Policy', () => {
  it('rejects a blanket default policy', () => {
    expect(() => expectRestrictiveRendererCsp('default-src *')).toThrow()
  })

  it('is present, restrictive, and permits audio from the embedded loopback server and data: images', () => {
    const html = readFileSync(new URL('./index.html', import.meta.url), 'utf8')
    const match = html.match(/<meta\s+http-equiv="Content-Security-Policy"\s+content="([^"]+)"\s*\/>/)

    expect(match, 'renderer CSP meta tag').not.toBeNull()
    expectRestrictiveRendererCsp(match![1])
  })
})
