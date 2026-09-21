import Foundation

/// Persists the selected `ServerConfiguration` -- origin, and for a paired
/// local connection, its certificate pin -- across app relaunches (issue
/// #767 repair round). Deliberately separate from `CredentialStore`: the
/// bearer token stays Keychain-only; this only remembers *where* to point
/// it and *which* certificate to pin. Without this, `configuration` reverts
/// to nil on every relaunch, so the saved Keychain token (namespaced by
/// `ServerConfiguration.keychainAccount`, itself derived from the origin)
/// can never be located again, and a locally-paired user is forced through
/// pairing again even though the pin is still valid.
public protocol ConfigurationStore {
    func saveConfiguration(_ configuration: ServerConfiguration) throws
    func loadConfiguration() -> ServerConfiguration?
    func clearConfiguration()
}

public enum ConfigurationStoreError: Error {
    case encoding
}

/// Real on-device implementation, backed by `UserDefaults` -- this data is
/// not secret (an origin URL and a public-key fingerprint, never a
/// credential), so it does not need Keychain-grade protection.
public final class UserDefaultsConfigurationStore: ConfigurationStore {
    private let defaults: UserDefaults
    private let key: String

    public init(defaults: UserDefaults = .standard, key: String = "org.muesli.ios.serverConfiguration") {
        self.defaults = defaults
        self.key = key
    }

    public func saveConfiguration(_ configuration: ServerConfiguration) throws {
        do {
            let data = try JSONEncoder().encode(configuration)
            defaults.set(data, forKey: key)
        } catch {
            throw ConfigurationStoreError.encoding
        }
    }

    public func loadConfiguration() -> ServerConfiguration? {
        guard let data = defaults.data(forKey: key) else { return nil }
        return try? JSONDecoder().decode(ServerConfiguration.self, from: data)
    }

    public func clearConfiguration() {
        defaults.removeObject(forKey: key)
    }
}

/// In-memory test double -- unit tests never touch real `UserDefaults`.
public final class InMemoryConfigurationStore: ConfigurationStore {
    private var stored: ServerConfiguration?

    public init(_ configuration: ServerConfiguration? = nil) {
        self.stored = configuration
    }

    public func saveConfiguration(_ configuration: ServerConfiguration) throws {
        stored = configuration
    }

    public func loadConfiguration() -> ServerConfiguration? {
        stored
    }

    public func clearConfiguration() {
        stored = nil
    }
}
