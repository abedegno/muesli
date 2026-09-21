import Foundation

/// Issue #767: the versioned local-pairing QR/manual-entry payload. Mirrors
/// internal/iosaccess/pairing.go's `PairingPayload` and
/// src/main/iosAccess/pairing.ts exactly -- origin, SPKI SHA-256 fingerprint,
/// and a human verification phrase only. Never a password, token, or
/// session: pairing establishes endpoint trust, not identity.
public struct PairingPayload: Equatable {
    public static let currentVersion = 1

    public let version: Int
    public let origin: URL
    public let spkiSHA256Hex: String
    public let phrase: String
}

public enum PairingPayloadError: Error, Equatable {
    case malformed
    case unsupportedVersion(Int)
    case invalidOrigin
    case originNotPrivateNetwork
    case invalidFingerprint
    case missingPhrase
}

private struct RawPairingPayload: Decodable {
    let v: Int
    let origin: String
    let spki_sha256: String
    let phrase: String
}

public enum PairingPayloadDecoder {
    /// Validates and decodes a scanned/typed pairing payload: current
    /// version, syntactically valid private-network HTTPS origin (no
    /// path/query/fragment/credentials, literal RFC1918/ULA host), and a
    /// well-formed 32-byte hex SPKI fingerprint. Throws rather than
    /// returning a partially-trusted value on any failure.
    public static func decode(_ raw: String) throws -> PairingPayload {
        guard let data = raw.data(using: .utf8) else {
            throw PairingPayloadError.malformed
        }
        let decoded: RawPairingPayload
        do {
            decoded = try JSONDecoder().decode(RawPairingPayload.self, from: data)
        } catch {
            throw PairingPayloadError.malformed
        }
        guard decoded.v == PairingPayload.currentVersion else {
            throw PairingPayloadError.unsupportedVersion(decoded.v)
        }
        guard let origin = try? ServerConfigurationValidator.normalize(decoded.origin) else {
            throw PairingPayloadError.invalidOrigin
        }
        guard let host = origin.host, isPrivateNetworkLiteral(host) else {
            throw PairingPayloadError.originNotPrivateNetwork
        }
        guard isValidFingerprintHex(decoded.spki_sha256) else {
            throw PairingPayloadError.invalidFingerprint
        }
        guard !decoded.phrase.isEmpty else {
            throw PairingPayloadError.missingPhrase
        }
        return PairingPayload(
            version: decoded.v,
            origin: origin,
            spkiSHA256Hex: decoded.spki_sha256.lowercased(),
            phrase: decoded.phrase
        )
    }
}

/// Validates that `hex` is exactly 64 lowercase-or-uppercase hex characters
/// (a SHA-256 digest).
func isValidFingerprintHex(_ hex: String) -> Bool {
    guard hex.count == 64 else { return false }
    return hex.allSatisfy { $0.isHexDigit }
}

/// RFC1918 IPv4 or IPv6 ULA (fc00::/7) literal host, matching the exclusions
/// enforced by internal/iosaccess.EligiblePairs on the desktop side. A
/// bracketed IPv6 literal's brackets are already stripped by URLComponents
/// (`components.host` returns the bare address).
func isPrivateNetworkLiteral(_ host: String) -> Bool {
    if let v4 = parseIPv4(host) {
        return isRFC1918(v4)
    }
    if let v6 = parseIPv6(host) {
        return isULA(v6)
    }
    return false
}

private func parseIPv4(_ host: String) -> [UInt8]? {
    let parts = host.split(separator: ".", omittingEmptySubsequences: false)
    guard parts.count == 4 else { return nil }
    var bytes: [UInt8] = []
    for part in parts {
        guard let n = UInt16(part), n <= 255, !part.isEmpty else { return nil }
        bytes.append(UInt8(n))
    }
    return bytes
}

private func isRFC1918(_ v4: [UInt8]) -> Bool {
    if v4[0] == 10 { return true }
    if v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31 { return true }
    if v4[0] == 192 && v4[1] == 168 { return true }
    return false
}

/// Minimal IPv6 literal parser sufficient to classify eligibility (not a
/// general-purpose IPv6 library): expands `::`, rejects anything else
/// malformed.
private func parseIPv6(_ host: String) -> [UInt16]? {
    guard host.contains(":") else { return nil }
    let halves = host.components(separatedBy: "::")
    guard halves.count <= 2 else { return nil }

    func parseGroups(_ s: String) -> [UInt16]? {
        if s.isEmpty { return [] }
        var out: [UInt16] = []
        for part in s.split(separator: ":") {
            guard let v = UInt16(part, radix: 16) else { return nil }
            out.append(v)
        }
        return out
    }

    if halves.count == 1 {
        guard let groups = parseGroups(halves[0]), groups.count == 8 else { return nil }
        return groups
    }
    guard let head = parseGroups(halves[0]), let tail = parseGroups(halves[1]) else { return nil }
    let missing = 8 - head.count - tail.count
    guard missing >= 0 else { return nil }
    return head + Array(repeating: 0, count: missing) + tail
}

private func isULA(_ v6: [UInt16]) -> Bool {
    (v6[0] & 0xfe00) == 0xfc00
}
