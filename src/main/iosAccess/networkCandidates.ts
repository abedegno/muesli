/**
 * Local iOS access candidate enumeration (issue #767).
 *
 * Mirrors internal/iosaccess/network_candidates.go's eligibility rules
 * exactly (RFC 1918 IPv4 / IPv6 ULA fc00::/7 eligible; public, loopback,
 * wildcard, IPv4 link-local 169.254.0.0/16, and IPv6 link-local fe80::/10
 * excluded), verified independently against the same shared JSON vectors —
 * this module never calls into Go, and the Go package never calls into
 * this one.
 */

export type NetworkFamily = 'ipv4' | 'ipv6'

/** One eligible (interface name, address, family) triple. */
export interface NetworkCandidate {
  interfaceName: string
  address: string
  family: NetworkFamily
}

/** Full-triple equality defines candidate identity. */
export function candidatesEqual(a: NetworkCandidate, b: NetworkCandidate): boolean {
  return a.interfaceName === b.interfaceName && a.address === b.address && a.family === b.family
}

/**
 * RawInterface is one already-active, non-loopback interface as reported by
 * the OS (Node's os.networkInterfaces() shape), before eligibility
 * filtering. Addresses may be plain IPs or CIDR notation.
 */
export interface RawInterface {
  name: string
  addresses: string[]
}

function stripZoneAndPrefix(raw: string): string {
  const withoutZone = raw.split('%')[0]
  return withoutZone.split('/')[0]
}

function parseIPv4(text: string): number[] | null {
  const parts = text.split('.')
  if (parts.length !== 4) return null
  const bytes: number[] = []
  for (const part of parts) {
    if (!/^\d{1,3}$/.test(part)) return null
    const n = Number(part)
    if (n < 0 || n > 255) return null
    bytes.push(n)
  }
  return bytes
}

/** True for a syntactically valid IPv6 literal (best-effort, not exhaustive). */
function looksLikeIPv6(text: string): boolean {
  return text.includes(':') && /^[0-9a-fA-F:]+$/.test(text)
}

/** Expands an IPv6 literal to 8 groups of 16-bit numbers, honoring `::`. */
function expandIPv6(text: string): number[] | null {
  if (!looksLikeIPv6(text)) return null
  const halves = text.split('::')
  if (halves.length > 2) return null
  const parseGroups = (s: string): number[] | null => {
    if (s === '') return []
    const parts = s.split(':')
    const out: number[] = []
    for (const p of parts) {
      if (!/^[0-9a-fA-F]{1,4}$/.test(p)) return null
      out.push(parseInt(p, 16))
    }
    return out
  }
  if (halves.length === 1) {
    const groups = parseGroups(halves[0])
    return groups && groups.length === 8 ? groups : null
  }
  const head = parseGroups(halves[0])
  const tail = parseGroups(halves[1])
  if (!head || !tail) return null
  const missing = 8 - head.length - tail.length
  if (missing < 0) return null
  return [...head, ...new Array<number>(missing).fill(0), ...tail]
}

function classify(address: string): { family: NetworkFamily; eligible: boolean } | null {
  const stripped = stripZoneAndPrefix(address)

  const v4 = parseIPv4(stripped)
  if (v4) {
    if (v4[0] === 127) return { family: 'ipv4', eligible: false } // loopback
    if (v4[0] === 0) return { family: 'ipv4', eligible: false } // wildcard-ish
    if (v4[0] === 169 && v4[1] === 254) return { family: 'ipv4', eligible: false } // link-local
    const rfc1918 =
      v4[0] === 10 || (v4[0] === 172 && v4[1] >= 16 && v4[1] <= 31) || (v4[0] === 192 && v4[1] === 168)
    return { family: 'ipv4', eligible: rfc1918 }
  }

  const v6 = expandIPv6(stripped)
  if (v6) {
    const isLoopback = v6.every((g, i) => (i < 7 ? g === 0 : g === 1))
    const isUnspecified = v6.every((g) => g === 0)
    const isLinkLocal = (v6[0] & 0xffc0) === 0xfe80 // fe80::/10
    if (isLoopback || isUnspecified || isLinkLocal) return { family: 'ipv6', eligible: false }
    const isULA = (v6[0] & 0xfe00) === 0xfc00 // fc00::/7
    return { family: 'ipv6', eligible: isULA }
  }

  return null
}

/**
 * Normalizes an IPv4 literal that survived classify() to its canonical
 * dotted form (drops any leading zeros / zone / prefix the raw string had).
 */
function normalizeAddress(address: string, family: NetworkFamily): string {
  const stripped = stripZoneAndPrefix(address)
  if (family === 'ipv4') {
    const v4 = parseIPv4(stripped)
    return v4 ? v4.join('.') : stripped
  }
  // IPv6 candidates always originate from already-eligible ULA literals in
  // this codebase's inputs; preserve the author's textual form rather than
  // re-compressing it, since Go's net.IP.String() and this function only
  // need to agree on eligibility, not on a canonical compressed spelling.
  return stripped
}

/**
 * Derives every eligible NetworkCandidate from a raw interface snapshot, per
 * the accepted spec for issue #767. One candidate is produced per eligible
 * interface-address pair, in input order.
 */
export function eligibleCandidates(interfaces: RawInterface[]): NetworkCandidate[] {
  const out: NetworkCandidate[] = []
  for (const iface of interfaces) {
    for (const raw of iface.addresses) {
      const result = classify(raw)
      if (!result || !result.eligible) continue
      out.push({
        interfaceName: iface.name,
        address: normalizeAddress(raw, result.family),
        family: result.family,
      })
    }
  }
  return out
}

/** The zero/one/many selection outcome for an "Allow iOS access" attempt. */
export interface SelectionOutcome {
  candidates: NetworkCandidate[]
  /** Set only when exactly one candidate exists. */
  autoSelected: NetworkCandidate | null
}

export function evaluateSelection(candidates: NetworkCandidate[]): SelectionOutcome {
  return {
    candidates,
    autoSelected: candidates.length === 1 ? candidates[0] : null,
  }
}
