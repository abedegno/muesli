import { createHash } from 'node:crypto'
import { describe, expect, it } from 'vitest'
import { buildOrigin, buildPairingPayload, decodePairingPayload, encodePairingPayload, verificationPhrase } from './pairing'

describe('buildOrigin', () => {
  it('leaves IPv4 bare', () => {
    expect(buildOrigin('192.168.1.20', 8443)).toBe('https://192.168.1.20:8443')
  })

  it('brackets IPv6 per RFC 3986', () => {
    expect(buildOrigin('fd12:3456:789a:1::20', 8443)).toBe('https://[fd12:3456:789a:1::20]:8443')
  })
})

describe('verificationPhrase', () => {
  it('matches an independently-computed fixture vector (cross-checks the Go implementation)', () => {
    // sha256("issue-767-pairing-test-vector"), first 5 bytes, base32
    // (RFC 4648, no padding), upper-cased, split 4/4 — computed offline with
    // Python's stdlib (base64.b32encode) as an implementation-independent
    // oracle, matching internal/iosaccess.VerificationPhrase's algorithm.
    const digest = createHash('sha256').update('issue-767-pairing-test-vector').digest('hex')
    expect(digest).toBe('27ddfcc3ead35bbaf7e5f8ced13ab11d7c53775ecfaba47cb29604865b3a52ba')
    expect(verificationPhrase(digest)).toBe('E7O7-ZQ7K')
  })

  it('is deterministic', () => {
    const digest = createHash('sha256').update('another-fixture').digest('hex')
    expect(verificationPhrase(digest)).toBe(verificationPhrase(digest))
  })

  it('differs for different fingerprints', () => {
    const a = createHash('sha256').update('fixture-a').digest('hex')
    const b = createHash('sha256').update('fixture-b').digest('hex')
    expect(verificationPhrase(a)).not.toBe(verificationPhrase(b))
  })
})

function realFingerprintHex(seed: string): string {
  return createHash('sha256').update(seed).digest('hex')
}

describe('buildPairingPayload / encodePairingPayload / decodePairingPayload', () => {
  it('round-trips and contains no forbidden secret-shaped content', () => {
    const fp = realFingerprintHex('pairing-round-trip')
    const origin = buildOrigin('192.168.1.20', 8443)
    const payload = buildPairingPayload(origin, fp)
    const raw = encodePairingPayload(payload)

    for (const forbidden of ['password', 'token', 'session', 'BEGIN ']) {
      expect(raw.toLowerCase()).not.toContain(forbidden.toLowerCase())
    }

    const decoded = decodePairingPayload(raw)
    expect(decoded).toEqual(payload)
  })

  it.each([
    ['not json', 'not json at all'],
    ['wrong version', JSON.stringify({ v: 99, origin: 'https://192.168.1.20:8443', spki_sha256: realFingerprintHex('x'), phrase: 'AB-CD' })],
    ['http not https', JSON.stringify({ v: 1, origin: 'http://192.168.1.20:8443', spki_sha256: realFingerprintHex('x'), phrase: 'AB-CD' })],
    ['public address', JSON.stringify({ v: 1, origin: 'https://8.8.8.8:8443', spki_sha256: realFingerprintHex('x'), phrase: 'AB-CD' })],
    ['hostname not ip', JSON.stringify({ v: 1, origin: 'https://example.com:8443', spki_sha256: realFingerprintHex('x'), phrase: 'AB-CD' })],
    ['path present', JSON.stringify({ v: 1, origin: 'https://192.168.1.20:8443/pair', spki_sha256: realFingerprintHex('x'), phrase: 'AB-CD' })],
    ['query present', JSON.stringify({ v: 1, origin: 'https://192.168.1.20:8443?x=1', spki_sha256: realFingerprintHex('x'), phrase: 'AB-CD' })],
    ['embedded creds', JSON.stringify({ v: 1, origin: 'https://user:pass@192.168.1.20:8443', spki_sha256: realFingerprintHex('x'), phrase: 'AB-CD' })],
    ['short fingerprint', JSON.stringify({ v: 1, origin: 'https://192.168.1.20:8443', spki_sha256: 'abcd', phrase: 'AB-CD' })],
    ['missing phrase', JSON.stringify({ v: 1, origin: 'https://192.168.1.20:8443', spki_sha256: realFingerprintHex('x'), phrase: '' })],
  ])('rejects: %s', (_name, raw) => {
    expect(() => decodePairingPayload(raw)).toThrow()
  })

  it('accepts an IPv6 origin', () => {
    const fp = realFingerprintHex('ipv6-origin')
    const origin = buildOrigin('fd12:3456:789a:1::20', 8443)
    const payload = buildPairingPayload(origin, fp)
    const decoded = decodePairingPayload(encodePairingPayload(payload))
    expect(decoded.origin).toBe(origin)
  })
})
