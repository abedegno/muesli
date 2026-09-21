import Foundation
#if canImport(Security)
    import Security
#endif

/// Abstracts Keychain-backed bearer-token storage (issue #767), namespaced
/// per `ServerConfiguration.keychainAccount` so switching servers never
/// reads another server's token. The password is never persisted anywhere
/// -- only the bearer token this app receives from `POST /api/login`.
public protocol CredentialStore {
    func saveToken(_ token: String, account: String) throws
    func loadToken(account: String) -> String?
    func deleteToken(account: String) throws
}

public enum CredentialStoreError: Error {
    case keychain(OSStatus)
}

#if canImport(Security)
    /// Real Keychain-backed implementation. Each account is a distinct
    /// generic-password item (`kSecClassGenericPassword`), service fixed to
    /// the app's bundle namespace, account the caller-supplied
    /// `ServerConfiguration.keychainAccount`.
    public final class KeychainCredentialStore: CredentialStore {
        private let service: String

        public init(service: String = "org.muesli.ios.credentials") {
            self.service = service
        }

        public func saveToken(_ token: String, account: String) throws {
            let data = Data(token.utf8)
            var query = baseQuery(account: account)
            query[kSecValueData as String] = data
            query[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock

            let status = SecItemAdd(query as CFDictionary, nil)
            if status == errSecDuplicateItem {
                let searchQuery = baseQuery(account: account)
                let updateStatus = SecItemUpdate(
                    searchQuery as CFDictionary,
                    [kSecValueData as String: data] as CFDictionary)
                guard updateStatus == errSecSuccess else {
                    throw CredentialStoreError.keychain(updateStatus)
                }
                return
            }
            guard status == errSecSuccess else {
                throw CredentialStoreError.keychain(status)
            }
        }

        public func loadToken(account: String) -> String? {
            var query = baseQuery(account: account)
            query[kSecReturnData as String] = true
            query[kSecMatchLimit as String] = kSecMatchLimitOne

            var result: AnyObject?
            let status = SecItemCopyMatching(query as CFDictionary, &result)
            guard status == errSecSuccess, let data = result as? Data else { return nil }
            return String(data: data, encoding: .utf8)
        }

        public func deleteToken(account: String) throws {
            let query = baseQuery(account: account)
            let status = SecItemDelete(query as CFDictionary)
            guard status == errSecSuccess || status == errSecItemNotFound else {
                throw CredentialStoreError.keychain(status)
            }
        }

        private func baseQuery(account: String) -> [String: Any] {
            [
                kSecClass as String: kSecClassGenericPassword,
                kSecAttrService as String: service,
                kSecAttrAccount as String: account,
            ]
        }
    }
#endif

/// In-memory test double -- unit tests never touch the real Keychain.
public final class InMemoryCredentialStore: CredentialStore {
    private var storage: [String: String] = [:]

    public init() {}

    public func saveToken(_ token: String, account: String) throws {
        storage[account] = token
    }

    public func loadToken(account: String) -> String? {
        storage[account]
    }

    public func deleteToken(account: String) throws {
        storage.removeValue(forKey: account)
    }
}
