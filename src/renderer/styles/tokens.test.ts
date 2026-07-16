import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const css = readFileSync(resolve(__dirname, './tokens.css'), 'utf8')

describe('tokens.css color-scheme', () => {
  it('declares color-scheme: light in :root, so native controls (e.g. the transcript <audio> player) render with light chrome in light mode', () => {
    const rootBlock = css.match(/:root\s*{([^}]*)}/)?.[1] ?? ''
    expect(rootBlock).toMatch(/color-scheme:\s*light/)
  })

  it('declares color-scheme: dark in .dark, so native controls render dark instead of the browser-default light UI bleeding through dark panels', () => {
    const darkBlock = css.match(/\.dark\s*{([^}]*)}/)?.[1] ?? ''
    expect(darkBlock).toMatch(/color-scheme:\s*dark/)
  })
})
