/**
 * Local iOS pairing payload construction/validation (issue #767). Mirrors
 * internal/iosaccess/pairing.go's shape and validation rules — pairing
 * establishes endpoint trust only, so the payload carries no password,
 * token, or session, ever.
 */

const PAIRING_PAYLOAD_VERSION = 1

export interface PairingPayload {
  v: number
  origin: string
  spki_sha256: string
  phrase: string
}

/** RFC 3986 bracket notation for IPv6 literals; IPv4 is used bare. */
export function buildOrigin(address: string, port: number): string {
  const isIPv6 = address.includes(':')
  const host = isIPv6 ? `[${address}]` : address
  return `https://${host}:${port}`
}

/**
 * Deterministically derives a short, human-readable verification phrase from
 * a SPKI SHA-256 fingerprint (hex string). Must match
 * internal/iosaccess.VerificationPhrase's derivation exactly: base32
 * (RFC 4648, no padding) of the fingerprint's first 5 bytes, upper-cased,
 * split 4/4 with a hyphen.
 */
export function verificationPhrase(fingerprintHex: string): string {
  const bytes = hexToBytes(fingerprintHex).slice(0, 5)
  const encoded = base32Encode(bytes).toUpperCase()
  if (encoded.length < 8) return encoded
  return `${encoded.slice(0, 4)}-${encoded.slice(4, 8)}`
}

function hexToBytes(hex: string): Uint8Array {
  const out = new Uint8Array(Math.floor(hex.length / 2))
  for (let i = 0; i < out.length; i++) {
    out[i] = parseInt(hex.substring(i * 2, i * 2 + 2), 16)
  }
  return out
}

const BASE32_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'

function base32Encode(bytes: Uint8Array): string {
  let bits = 0
  let value = 0
  let out = ''
  for (const byte of bytes) {
    value = (value << 8) | byte
    bits += 8
    while (bits >= 5) {
      out += BASE32_ALPHABET[(value >>> (bits - 5)) & 0x1f]
      bits -= 5
    }
  }
  if (bits > 0) {
    out += BASE32_ALPHABET[(value << (5 - bits)) & 0x1f]
  }
  return out
}

export function buildPairingPayload(origin: string, spkiSha256Hex: string): PairingPayload {
  return {
    v: PAIRING_PAYLOAD_VERSION,
    origin,
    spki_sha256: spkiSha256Hex.toLowerCase(),
    phrase: verificationPhrase(spkiSha256Hex),
  }
}

export function encodePairingPayload(payload: PairingPayload): string {
  return JSON.stringify(payload)
}

const RFC1918_OR_ULA = /^(10\.|172\.(1[6-9]|2\d|3[01])\.|192\.168\.)/

function isPrivateIPv4Literal(host: string): boolean {
  return RFC1918_OR_ULA.test(host)
}

function isPrivateIPv6Literal(host: string): boolean {
  const stripped = host.replace(/^\[/, '').replace(/\]$/, '')
  if (!stripped.includes(':')) return false
  const firstGroup = stripped.split(':')[0]
  if (firstGroup.length < 2) return false
  const value = parseInt(firstGroup.substring(0, 2), 16)
  if (Number.isNaN(value)) return false
  return (value & 0xfe) === 0xfc // fc00::/7
}

class PairingValidationError extends Error {}

function validatePrivateHTTPSOrigin(origin: string): void {
  let url: URL
  try {
    url = new URL(origin)
  } catch {
    throw new PairingValidationError('invalid origin')
  }
  if (url.protocol !== 'https:') throw new PairingValidationError('origin must be https')
  if (url.pathname !== '' && url.pathname !== '/') throw new PairingValidationError('origin must not contain a path')
  if (url.search !== '') throw new PairingValidationError('origin must not contain a query')
  if (url.hash !== '') throw new PairingValidationError('origin must not contain a fragment')
  if (url.username !== '' || url.password !== '') throw new PairingValidationError('origin must not embed credentials')
  if (url.port === '') throw new PairingValidationError('origin must include a port')

  const host = url.hostname
  const isEligible = isPrivateIPv4Literal(host) || isPrivateIPv6Literal(host)
  if (!isEligible) throw new PairingValidationError('origin host must be a private-network literal address')
}

/**
 * Validates and parses a scanned/typed pairing payload. Throws with a
 * human-readable message on any malformed, wrong-versioned, or
 * non-private-origin payload — the caller (pairing UI) must not save
 * anything from a payload this rejects.
 */
export function decodePairingPayload(raw: string): PairingPayload {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    throw new PairingValidationError('malformed pairing payload')
  }
  if (typeof parsed !== 'object' || parsed === null) {
    throw new PairingValidationError('malformed pairing payload')
  }
  const p = parsed as Partial<PairingPayload>
  if (p.v !== PAIRING_PAYLOAD_VERSION) {
    throw new PairingValidationError(`unsupported pairing payload version ${String(p.v)}`)
  }
  if (typeof p.origin !== 'string') throw new PairingValidationError('missing origin')
  validatePrivateHTTPSOrigin(p.origin)
  if (typeof p.spki_sha256 !== 'string' || !/^[0-9a-fA-F]{64}$/.test(p.spki_sha256)) {
    throw new PairingValidationError('invalid fingerprint')
  }
  if (typeof p.phrase !== 'string' || p.phrase === '') {
    throw new PairingValidationError('missing verification phrase')
  }
  return { v: p.v, origin: p.origin, spki_sha256: p.spki_sha256, phrase: p.phrase }
}
