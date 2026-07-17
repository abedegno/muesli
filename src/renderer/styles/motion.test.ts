import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const css = readFileSync(resolve(__dirname, './index.css'), 'utf8')

describe('index.css reduced-motion', () => {
  it('declares a prefers-reduced-motion: reduce media query', () => {
    expect(css).toMatch(/@media\s*\(prefers-reduced-motion:\s*reduce\)/)
  })

  it('neutralizes animations and transitions inside that query with !important', () => {
    const idx = css.indexOf('@media (prefers-reduced-motion: reduce)')
    expect(idx).toBeGreaterThan(-1)
    const after = css.slice(idx)
    expect(after).toMatch(/animation-duration:\s*0\.01ms\s*!important/)
    expect(after).toMatch(/animation-iteration-count:\s*1\s*!important/)
    expect(after).toMatch(/transition-duration:\s*0\.01ms\s*!important/)
  })
})
