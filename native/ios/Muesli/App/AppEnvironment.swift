import Foundation

/// Composition root (issue #767): constructs the injected stores/clients
/// once per app launch. Views receive these via SwiftUI environment objects
/// -- no view ever constructs a CredentialStore, URLSession, or APIClient
/// itself.
@MainActor
public final class AppEnvironment: ObservableObject {
    public let credentialStore: CredentialStore
    public let sessionStore: SessionStore
    @Published public var configuration: ServerConfiguration?

    public init(credentialStore: CredentialStore) {
        self.credentialStore = credentialStore
        self.sessionStore = SessionStore(credentialStore: credentialStore)
    }

    #if canImport(Security)
        public static func live() -> AppEnvironment {
            AppEnvironment(credentialStore: KeychainCredentialStore())
        }
    #endif

    public func makeNotesListModel() -> NotesListModel? {
        guard let configuration else { return nil }
        let model = NotesListModel(client: sessionStore.makeAPIClient(configuration: configuration))
        model.onSessionEnded = { [weak self] in
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
        return model
    }
}
