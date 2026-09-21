import Foundation

/// Composition root (issue #767): constructs the injected stores/clients
/// once per app launch. Views receive these via SwiftUI environment objects
/// -- no view ever constructs a CredentialStore, URLSession, or APIClient
/// itself.
@MainActor
public final class AppEnvironment: ObservableObject {
    public let credentialStore: CredentialStore
    public let sessionStore: SessionStore
    private let configurationStore: ConfigurationStore

    /// The selected server (hosted or paired-local). Persisted via
    /// `configurationStore` on every change (issue #767 repair round) so a
    /// relaunch never silently reverts to nil -- see `ConfigurationStore`'s
    /// doc comment for why that mattered.
    @Published public var configuration: ServerConfiguration? {
        didSet {
            if let configuration {
                try? configurationStore.saveConfiguration(configuration)
            } else {
                configurationStore.clearConfiguration()
            }
        }
    }

    public init(
        credentialStore: CredentialStore, configurationStore: ConfigurationStore = InMemoryConfigurationStore()
    ) {
        self.credentialStore = credentialStore
        self.configurationStore = configurationStore
        self.sessionStore = SessionStore(credentialStore: credentialStore)
        // Restore the persisted configuration -- and, if a token is still
        // saved for it, the signed-in session -- before the root view ever
        // renders, so a relaunch with a saved token (and, for a local
        // connection, its still-valid pin) re-enters the notes list instead
        // of bouncing back to sign-in/pairing.
        if let restored = configurationStore.loadConfiguration() {
            self.configuration = restored
            self.sessionStore.restoreIfPossible(configuration: restored)
        }
    }

    #if canImport(Security)
        public static func live() -> AppEnvironment {
            AppEnvironment(
                credentialStore: KeychainCredentialStore(),
                configurationStore: UserDefaultsConfigurationStore())
        }
    #endif

    public func makeNotesListModel() -> NotesListModel? {
        guard let configuration else { return nil }
        let model = NotesListModel(client: sessionStore.makeAPIClient(configuration: configuration))
        model.onSessionEnded = { [weak self] in
            guard let self, let configuration = self.configuration else { return }
            self.sessionStore.sessionEnded(configuration: configuration)
        }
        // A local-trust change means the previously pinned certificate is no
        // longer valid, so the paired session can no longer be trusted --
        // same destination/effect as an authenticated 401: clear
        // credentials and route back to pairing/sign-in.
        model.onLocalTrustChanged = { [weak self] in
            guard let self, let configuration = self.configuration else { return }
            self.sessionStore.sessionEnded(configuration: configuration)
        }
        return model
    }

    public func makeNoteReaderModel(noteID: String) -> NoteReaderModel? {
        guard let configuration else { return nil }
        let model = NoteReaderModel(
            client: sessionStore.makeAPIClient(configuration: configuration), noteID: noteID)
        model.onSessionEnded = { [weak self] in
            guard let self, let configuration = self.configuration else { return }
            self.sessionStore.sessionEnded(configuration: configuration)
        }
        // Same as above: a local-trust change routes back to pairing/sign-in
        // the same way a session-ended (401) does.
        model.onLocalTrustChanged = { [weak self] in
            guard let self, let configuration = self.configuration else { return }
            self.sessionStore.sessionEnded(configuration: configuration)
        }
        return model
    }
}
