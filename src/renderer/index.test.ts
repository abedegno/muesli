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
}

describe('renderer Content Security Policy', () => {
  it('rejects a blanket default policy', () => {
    expect(() => expectRestrictiveRendererCsp('default-src *')).toThrow()
  })

  it('is present, restrictive, and permits audio from the embedded loopback server', () => {
    const html = readFileSync(new URL('./index.html', import.meta.url), 'utf8')
    const match = html.match(/<meta\s+http-equiv="Content-Security-Policy"\s+content="([^"]+)"\s*\/>/)

    expect(match, 'renderer CSP meta tag').not.toBeNull()
    expectRestrictiveRendererCsp(match![1])
  })
})
