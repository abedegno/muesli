import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('lighthouserc.json', () => {
  it('does not publish accessibility reports to public storage', () => {
    const config = JSON.parse(readFileSync('lighthouserc.json', 'utf8'))
    expect(config.ci.upload.target).not.toBe('temporary-public-storage')
  })

  it('keeps the accessibility assertion at error severity', () => {
    const config = JSON.parse(readFileSync('lighthouserc.json', 'utf8'))
    expect(config.ci.assert.assertions['categories:accessibility'][0]).toBe('error')
  })
})
