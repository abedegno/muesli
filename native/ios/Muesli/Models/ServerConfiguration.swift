import Foundation

/// Issue #767: models either a hosted origin (validated, no pinning beyond
/// standard ATS) or a local origin pinned to one SPKI SHA-256 fingerprint
/// (established through pairing). Trust logic lives on `APIClient`'s
/// delegate, keyed off `pinnedSPKISHA256Hex` being non-nil — this type only
/// carries the data, never performs the handshake.
public struct ServerConfiguration: Equatable, Codable {
    /// Absolute HTTPS origin, no trailing slash, no path/query/fragment.
    public let origin: URL
    /// Present only for a local (paired) connection; nil for hosted.
    public let pinnedSPKISHA256Hex: String?

    public init(origin: URL, pinnedSPKISHA256Hex: String? = nil) {
        self.origin = origin
        self.pinnedSPKISHA256Hex = pinnedSPKISHA256Hex
    }

    public var isLocal: Bool { pinnedSPKISHA256Hex != nil }

    /// The Keychain account key this configuration's credentials are stored
    /// under -- namespaced by the normalized origin so switching servers
    /// never reads another server's token.
    public var keychainAccount: String {
        "muesli.origin." + origin.absoluteString
    }
}

/// Errors surfaced while normalizing/validating a user-entered hosted server
/// URL, per the accepted spec: absolute HTTPS origin only, no
/// path/query/fragment/credentials.
public enum ServerConfigurationError: Error, Equatable {
    case empty
    case notAbsolute
    case notHTTPS
    case containsPath
    case containsQueryOrFragment
    case containsCredentials
    case invalidHost
}

public enum ServerConfigurationValidator {
    /// Trims whitespace and a trailing slash, then accepts only an absolute
    /// HTTPS origin with no path, query, fragment, or embedded credentials.
    /// Release builds never weaken this to accept HTTP or user-supplied
    /// certificates through this path (that trust path is local pairing
    /// only, via `ServerConfiguration.pinnedSPKISHA256Hex`).
    public static func normalize(_ raw: String) throws -> URL {
        var trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { throw ServerConfigurationError.empty }
        while trimmed.hasSuffix("/") {
            trimmed.removeLast()
        }
        guard let components = URLComponents(string: trimmed) else {
            throw ServerConfigurationError.notAbsolute
        }
        guard components.scheme?.lowercased() == "https" else {
            throw ServerConfigurationError.notHTTPS
        }
        guard let host = components.host, !host.isEmpty else {
            throw ServerConfigurationError.invalidHost
        }
        if let user = components.user, !user.isEmpty {
            throw ServerConfigurationError.containsCredentials
        }
        if let password = components.password, !password.isEmpty {
            throw ServerConfigurationError.containsCredentials
        }
        let path = components.path
        if !path.isEmpty && path != "/" {
            throw ServerConfigurationError.containsPath
        }
        if let query = components.query, !query.isEmpty {
            throw ServerConfigurationError.containsQueryOrFragment
        }
        if let fragment = components.fragment, !fragment.isEmpty {
            throw ServerConfigurationError.containsQueryOrFragment
        }

        var normalized = URLComponents()
        normalized.scheme = "https"
        normalized.host = host
        normalized.port = components.port
        guard let url = normalized.url else {
            throw ServerConfigurationError.notAbsolute
        }
        return url
    }
}
