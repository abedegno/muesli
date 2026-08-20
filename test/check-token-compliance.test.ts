import { describe, expect, it } from 'vitest'
// @ts-expect-error The checker is intentionally a directly executable JavaScript module.
import { scanSource, validateExceptions } from '../scripts/check-token-compliance.mjs'

const NOW = new Date('2026-08-20T00:00:00Z')

type Violation = { file: string; line: number; rule: string; message: string; snippet: string }
const rules = (violations: Violation[]): string[] => violations.map((v) => v.rule)

function except(overrides: Record<string, unknown> = {}) {
  return {
    file: 'src/renderer/components/NoteScreen.tsx',
    rule: 'raw-palette-class',
    reason: 'warning surface awaiting semantic status tokens',
    owner: 'abedegno',
    expires: '2026-11-20',
    count: 1,
    ...overrides,
  }
}

describe('scanSource', () => {
  it('flags a colour token wrapped in hsl(), which is invalid CSS that silently falls back', () => {
    const v: Violation[] = scanSource('src/renderer/x.tsx', 'fill="hsl(var(--primary))"', [], NOW)
    expect(rules(v)).toEqual(['invalid-var-composition'])
    expect(v[0].line).toBe(1)
  })

  it('reports EVERY occurrence, not just the first, or fixing one only reveals the next', () => {
    const source = ['<a className="bg-amber-500" />', '<b className="text-slate-700" />'].join('\n')
    const v: Violation[] = scanSource('src/renderer/x.tsx', source, [], NOW)
    expect(rules(v)).toEqual(['raw-palette-class', 'raw-palette-class'])
    expect(v.map((x) => x.line)).toEqual([1, 2])
  })

  it('flags bg-black and bg-white overlays, which are palette colours too', () => {
    const v: Violation[] = scanSource(
      'src/renderer/x.tsx',
      '<a className="bg-black/40" />',
      [],
      NOW
    )
    expect(rules(v)).toEqual(['raw-palette-class'])
  })

  it('does not flag prose or identifiers that merely contain a hue name', () => {
    const source = '// the blue-500 swatch was rejected\nconst blue500Note = 1'
    expect(scanSource('src/renderer/x.tsx', source, [], NOW)).toEqual([])
  })

  it('accepts semantic token classes, which are the whole point of the system', () => {
    const source = '<a className="bg-primary text-primary-foreground border-border" />'
    expect(scanSource('src/renderer/x.tsx', source, [], NOW)).toEqual([])
  })

  it('flags hex literals of every valid CSS length outside the token definitions', () => {
    const source = ['const a = "#abc"', 'const b = "#111827ff"', 'const c = "#f9fafb"'].join('\n')
    expect(rules(scanSource('src/renderer/lib/monogram.ts', source, [], NOW))).toEqual([
      'hex-literal',
      'hex-literal',
      'hex-literal',
    ])
  })

  it('does not flag the token definitions themselves, by absolute or relative path', () => {
    const line = '  --primary: #0d9488;'
    expect(scanSource('src/renderer/styles/tokens.css', line, [], NOW)).toEqual([])
    expect(scanSource('/Users/x/muesli/src/renderer/styles/tokens.css', line, [], NOW)).toEqual([])
  })

  it('does not scan test files, whose fixtures legitimately contain raw classes', () => {
    const source = '<a className="bg-amber-500" />'
    expect(scanSource('src/renderer/components/X.test.tsx', source, [], NOW)).toEqual([])
  })

  it('suppresses occurrences up to the recorded count', () => {
    const source = '<a className="bg-amber-500" />'
    expect(scanSource('src/renderer/components/NoteScreen.tsx', source, [except()], NOW)).toEqual(
      []
    )
  })

  it('flags a regression that pushes occurrences above the recorded count', () => {
    const source = ['<a className="bg-amber-500" />', '<b className="bg-rose-600" />'].join('\n')
    const v: Violation[] = scanSource(
      'src/renderer/components/NoteScreen.tsx',
      source,
      [except()],
      NOW
    )
    expect(rules(v)).toEqual(['exception-count-exceeded'])
  })

  it('flags a stale baseline so the ratchet tightens as offenders are fixed', () => {
    const v: Violation[] = scanSource(
      'src/renderer/components/NoteScreen.tsx',
      'const nothing = 1',
      [except({ count: 3 })],
      NOW
    )
    expect(rules(v)).toEqual(['exception-count-stale'])
  })

  it('does not let an exception for one rule suppress a different rule', () => {
    const v: Violation[] = scanSource(
      'src/renderer/components/NoteScreen.tsx',
      'fill="hsl(var(--primary))"',
      [except()],
      NOW
    )
    // The invalid-var-composition finding is unaffected by the unrelated
    // raw-palette-class exception, proving it isn't a blanket suppression.
    // That same exception legitimately reports as stale here too: its
    // recorded count is 1 but this source has zero raw-palette-class hits,
    // which is exactly the "count exceeds reality" case exercised on its own
    // by the "flags a stale baseline" test below.
    expect(rules(v)).toEqual(['invalid-var-composition', 'exception-count-stale'])
  })
})

describe('validateExceptions', () => {
  it('rejects an expired exception rather than letting the list rot', () => {
    expect(rules(validateExceptions([except({ expires: '2026-01-01' })], NOW))).toEqual([
      'expired-exception',
    ])
  })

  it('rejects an unparseable expiry, which would otherwise never expire', () => {
    expect(rules(validateExceptions([except({ expires: 'soon' })], NOW))).toEqual([
      'malformed-exception',
    ])
  })

  it('rejects a missing owner or reason', () => {
    expect(rules(validateExceptions([except({ owner: '' })], NOW))).toEqual(['malformed-exception'])
    expect(rules(validateExceptions([except({ reason: undefined })], NOW))).toEqual([
      'malformed-exception',
    ])
  })

  it('rejects an unknown rule id, which would silently suppress nothing', () => {
    expect(rules(validateExceptions([except({ rule: 'not-a-rule' })], NOW))).toEqual([
      'malformed-exception',
    ])
  })

  it('accepts a well-formed unexpired exception', () => {
    expect(validateExceptions([except()], NOW)).toEqual([])
  })
})
