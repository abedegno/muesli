import { describe, it, expect } from 'vitest'
import {
  checkA11yExceptions,
  exceededA11yExceptions,
  exemptRuleIds,
  type A11yException,
} from './a11yExceptions'

const NOW = new Date('2026-08-20T00:00:00Z')

function exception(overrides: Partial<A11yException> = {}): A11yException {
  return {
    rule: 'color-contrast',
    reason: 'the --primary token is below AA contrast; a design-language decision',
    owner: 'abedegno',
    expires: '2026-11-20',
    count: 3,
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

  it('flags a missing, non-integer or negative count as malformed', () => {
    const bad = [
      exception({ count: undefined as unknown as number }),
      exception({ count: -1 }),
      exception({ count: 1.5 }),
      exception({ count: '3' as unknown as number }),
    ]
    for (const entry of bad) {
      const problems = checkA11yExceptions([entry], NOW)
      expect(problems, JSON.stringify(entry)).toHaveLength(1)
      expect(problems[0].message).toMatch(/malformed a11y exception/)
    }
  })

  it('accepts a count of zero, which records that the rule must not occur at all', () => {
    expect(checkA11yExceptions([exception({ count: 0 })], NOW)).toEqual([])
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

describe('exceededA11yExceptions', () => {
  it('accepts an observed count equal to the recorded baseline', () => {
    expect(exceededA11yExceptions([{ rule: 'color-contrast', nodes: 3 }], [exception()], NOW))
      .toEqual([])
  })

  it('fails when one more violating node appears than the baseline records', () => {
    const problems = exceededA11yExceptions(
      [{ rule: 'color-contrast', nodes: 4 }],
      [exception()],
      NOW
    )
    expect(problems).toHaveLength(1)
    expect(problems[0].message).toMatch(/covers 3 node\(s\) but 4 were found/)
    expect(problems[0].message).toContain('color-contrast')
    expect(problems[0].message).toContain('abedegno')
  })

  it('does not fail when fewer nodes are found than the baseline records', () => {
    // The opposite of the token scanner, and deliberately so: a rendered node
    // count moves with fonts and window size, so failing on an improvement
    // would make the floor flaky in the one direction that is good news.
    expect(exceededA11yExceptions([{ rule: 'color-contrast', nodes: 1 }], [exception()], NOW))
      .toEqual([])
  })

  it('sums nodes across every observation carrying the same rule id', () => {
    const problems = exceededA11yExceptions(
      [
        { rule: 'color-contrast', nodes: 2 },
        { rule: 'color-contrast', nodes: 2 },
      ],
      [exception()],
      NOW
    )
    expect(problems).toHaveLength(1)
    expect(problems[0].message).toMatch(/but 4 were found/)
  })

  it('ignores rules that have no exception at all -- those fail as violations instead', () => {
    expect(exceededA11yExceptions([{ rule: 'button-name', nodes: 9 }], [exception()], NOW)).toEqual(
      []
    )
  })

  it('does not apply the baseline of an expired or malformed exception', () => {
    // Such an exception protects nothing, so its rule is never exempted and
    // the violation fails on its own. Reporting an excess as well would name
    // the same debt twice.
    const expired = exception({ expires: '2020-01-01' })
    const malformed = exception({ owner: '' })
    expect(exceededA11yExceptions([{ rule: 'color-contrast', nodes: 9 }], [expired], NOW)).toEqual(
      []
    )
    expect(exceededA11yExceptions([{ rule: 'color-contrast', nodes: 9 }], [malformed], NOW)).toEqual(
      []
    )
  })

  it('reports nothing when the rule was not observed at all', () => {
    expect(exceededA11yExceptions([], [exception()], NOW)).toEqual([])
  })
})
