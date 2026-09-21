import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  candidatesEqual,
  eligibleCandidates,
  evaluateSelection,
  type NetworkCandidate,
  type RawInterface,
} from './networkCandidates'

interface FixtureCase {
  name: string
  interfaces: RawInterface[]
  expected: NetworkCandidate[]
}

function loadFixture(): FixtureCase[] {
  const raw = readFileSync(join(__dirname, '../../../testdata/ios_access_network_candidates.json'), 'utf8')
  return (JSON.parse(raw) as { cases: FixtureCase[] }).cases
}

describe('eligibleCandidates (shared Go/TypeScript vectors, issue #767)', () => {
  for (const testCase of loadFixture()) {
    it(testCase.name, () => {
      expect(eligibleCandidates(testCase.interfaces)).toEqual(testCase.expected)
    })
  }
})

describe('evaluateSelection', () => {
  it('zero candidates', () => {
    const out = evaluateSelection([])
    expect(out.autoSelected).toBeNull()
    expect(out.candidates).toEqual([])
  })

  it('one candidate auto-selects', () => {
    const one: NetworkCandidate = { interfaceName: 'en0', address: '192.168.1.20', family: 'ipv4' }
    const out = evaluateSelection([one])
    expect(out.autoSelected).toEqual(one)
  })

  it('many candidates require explicit selection', () => {
    const many: NetworkCandidate[] = [
      { interfaceName: 'en0', address: '192.168.1.20', family: 'ipv4' },
      { interfaceName: 'en1', address: '192.168.86.5', family: 'ipv4' },
    ]
    const out = evaluateSelection(many)
    expect(out.autoSelected).toBeNull()
    expect(out.candidates).toHaveLength(2)
  })
})

describe('candidatesEqual', () => {
  const base: NetworkCandidate = { interfaceName: 'en0', address: '192.168.1.20', family: 'ipv4' }

  it('is true for an identical triple', () => {
    expect(candidatesEqual(base, { ...base })).toBe(true)
  })

  it('is false when only the interface differs (address moved interfaces)', () => {
    expect(candidatesEqual(base, { ...base, interfaceName: 'utun0' })).toBe(false)
  })

  it('is false when only the address differs', () => {
    expect(candidatesEqual(base, { ...base, address: '192.168.1.21' })).toBe(false)
  })

  it('is false when only the family differs', () => {
    expect(candidatesEqual(base, { ...base, family: 'ipv6' })).toBe(false)
  })
})
