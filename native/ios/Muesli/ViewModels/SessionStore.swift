import Foundation

/// Owns the signed-in session for one `ServerConfiguration` (issue #767).
/// State is per-instance -- never a singleton/global -- so there is exactly
/// one source of truth for "are we signed in" per app launch.
@MainActor
public final class SessionStore: ObservableObject {
    public enum State: Equatable {
        case signedOut(reason: String?)
        case signingIn
        case signedIn(ServerConfiguration)
    }

    @Published public private(set) var state: State = .signedOut(reason: nil)
    @Published public private(set) var lastEmail: String?

    private let credentialStore: CredentialStore
    private let apiClientFactory: (ServerConfiguration, @escaping () -> String?) -> APIClient

    public init(
        credentialStore: CredentialStore,
        apiClientFactory: @escaping (ServerConfiguration, @escaping () -> String?) -> APIClient = { config, token in
            APIClient(configuration: config, tokenProvider: token)
        }
    ) {
        self.credentialStore = credentialStore
        self.apiClientFactory = apiClientFactory
    }

    /// On launch, a saved origin/token pair enters directly (the notes
    /// screen validates it on the first list request; an authenticated 401
    /// there calls `signOut(reason:)`).
    public func restoreIfPossible(configuration: ServerConfiguration) {
        guard credentialStore.loadToken(account: configuration.keychainAccount) != nil else { return }
        state = .signedIn(configuration)
    }

    public func signIn(configuration: ServerConfiguration, email: String, password: String) async {
        state = .signingIn
        let client = apiClientFactory(configuration) { nil }
        do {
            let token = try await client.login(email: email, password: password)
            try credentialStore.saveToken(token, account: configuration.keychainAccount)
            lastEmail = email
            state = .signedIn(configuration)
        } catch {
            // Invalid login preserves origin/email (the sign-in screen keeps
            // showing what the person typed); only the in-flight attempt
            // fails.
            lastEmail = email
            state = .signedOut(reason: Self.message(for: error))
        }
    }

    public func makeAPIClient(configuration: ServerConfiguration) -> APIClient {
        apiClientFactory(configuration) { [credentialStore] in
            credentialStore.loadToken(account: configuration.keychainAccount)
        }
    }

    /// Sign-out and an authenticated 401 both delete the token and return to
    /// sign-in; only the message differs.
    public func signOut(configuration: ServerConfiguration) {
        try? credentialStore.deleteToken(account: configuration.keychainAccount)
        state = .signedOut(reason: nil)
    }

    public func sessionEnded(configuration: ServerConfiguration) {
        try? credentialStore.deleteToken(account: configuration.keychainAccount)
        state = .signedOut(reason: "Your session ended. Please sign in again.")
    }

    private static func message(for error: Error) -> String {
        if let apiError = error as? APIClientError {
            switch apiError {
            case .server(_, let message): return message
            case .transport(let message): return message
            case .decoding: return "Unexpected response from server."
            case .notAuthenticated: return "Not authenticated."
            case .sessionEnded: return "Your session ended. Please sign in again."
            case .localTrustChanged: return "The local connection changed. Scan the code again."
            }
        }
        return "Unable to sign in."
    }
}
