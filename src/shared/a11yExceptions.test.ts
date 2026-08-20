import { describe, it, expect } from 'vitest'
import { checkA11yExceptions, exemptRuleIds, type A11yException } from './a11yExceptions'

const NOW = new Date('2026-08-20T00:00:00Z')

function exception(overrides: Partial<A11yException> = {}): A11yException {
  return {
    rule: 'color-contrast',
    reason: 'the --primary token is below AA contrast; a design-language decision',
    owner: 'abedegno',
    expires: '2026-11-20',
    ...overrides,
  }
}

describe('checkA11yExceptions', () => {
  it('reports no problems for a valid, unexpired exception', () => {
    expect(checkA11yExceptions([exception()], NOW)).toEqual([])
  })

  it('flags an exception that expires exactly at `now` (inclusive boundary)', () => {
    const e = exception({ expires: '2026-08-20T00:00:00Z' })
    const problems = checkA11yExceptions([e], NOW)
    expect(problems).toHaveLength(1)
    expect(problems[0].exception).toBe(e)
    expect(problems[0].message).toContain('expired on 2026-08-20T00:00:00Z')
    expect(problems[0].message).toContain('color-contrast')
    expect(problems[0].message).toContain('abedegno')
  })

  it('flags an exception that expired in the past', () => {
    const problems = checkA11yExceptions([exception({ expires: '2020-01-01' })], NOW)
    expect(problems).toHaveLength(1)
    expect(problems[0].message).toMatch(/expired on 2020-01-01/)
  })

  it('does not flag an exception that expires in the future', () => {
    expect(checkA11yExceptions([exception({ expires: '2099-01-01' })], NOW)).toEqual([])
  })

  it('flags a missing or empty reason as malformed', () => {
    for (const bad of [exception({ reason: '' }), exception({ reason: '   ' })]) {
      const problems = checkA11yExceptions([bad], NOW)
      expect(problems).toHaveLength(1)
      expect(problems[0].message).toMatch(/malformed a11y exception/)
    }
  })

  it('flags a missing or empty owner as malformed', () => {
    const problems = checkA11yExceptions([exception({ owner: '' })], NOW)
    expect(problems).toHaveLength(1)
    expect(problems[0].message).toMatch(/malformed a11y exception/)
  })

  it('flags a missing or empty rule as malformed', () => {
    const problems = checkA11yExceptions([exception({ rule: '' })], NOW)
    expect(problems).toHaveLength(1)
    expect(problems[0].message).toMatch(/malformed a11y exception/)
  })

  it('flags an unparseable expires date as malformed rather than treating it as expired or permanent', () => {
    for (const bad of ['not-a-date', '', '2026-13-45']) {
      const problems = checkA11yExceptions([exception({ expires: bad })], NOW)
      expect(problems, bad).toHaveLength(1)
      expect(problems[0].message, bad).toMatch(/malformed a11y exception/)
    }
  })

  it('treats a non-object entry as malformed rather than throwing (defends non-TS callers, e.g. a future JSON-loaded list)', () => {
    const problems = checkA11yExceptions([null as unknown as A11yException], NOW)
    expect(problems).toHaveLength(1)
    expect(problems[0].message).toMatch(/malformed a11y exception/)
  })

  it('reports one problem per bad exception and none for good ones, in a mixed list', () => {
    const good = exception()
    const expired = exception({ rule: 'button-name', expires: '2020-01-01' })
    const malformed = exception({ rule: 'label', owner: '' })
    const problems = checkA11yExceptions([good, expired, malformed], NOW)
    expect(problems).toHaveLength(2)
    expect(problems.map((p) => p.exception)).toEqual([expired, malformed])
  })
})

describe('exemptRuleIds', () => {
  it('exempts the rule of a valid exception', () => {
    expect(exemptRuleIds([exception({ rule: 'color-contrast' })], NOW)).toEqual(
      new Set(['color-contrast'])
    )
  })

  it('does NOT exempt the rule of an expired exception -- an expired exception protects nothing', () => {
    const expired = exception({ rule: 'color-contrast', expires: '2020-01-01' })
    expect(exemptRuleIds([expired], NOW)).toEqual(new Set())
  })

  it('does NOT exempt the rule of a malformed exception', () => {
    const malformed = exception({ rule: 'color-contrast', owner: '' })
    expect(exemptRuleIds([malformed], NOW)).toEqual(new Set())
  })

  it('never exempts a rule that has no exception for it at all', () => {
    expect(exemptRuleIds([exception({ rule: 'color-contrast' })], NOW).has('button-name')).toBe(
      false
    )
  })

  it('exempts only the rules with a valid exception out of a mixed list', () => {
    const good = exception({ rule: 'color-contrast' })
    const expired = exception({ rule: 'button-name', expires: '2020-01-01' })
    expect(exemptRuleIds([good, expired], NOW)).toEqual(new Set(['color-contrast']))
  })
})
