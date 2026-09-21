import XCTest
@testable import Muesli

/// Proves the actual `AppEnvironment` wiring (issue #767 repair round), not
/// a bare closure stub: `makeNotesListModel()`/`makeNoteReaderModel()` wire
/// `onLocalTrustChanged` to the same pairing/sign-in transition
/// `onSessionEnded` already uses (`SessionStore.sessionEnded(configuration:)`),
/// since a local-trust change means the previously pinned certificate can no
/// longer be trusted. Model-level tests (NotesListModelTests,
/// NoteReaderModelTests) already prove a real `.localTrustChanged` network
/// failure fires `onLocalTrustChanged`; this file proves what firing that
/// callback, through the real `AppEnvironment`-constructed closure, actually
/// does to session state and the stored credential.
@MainActor
final class AppEnvironmentTests: XCTestCase {
    let localConfig = ServerConfiguration(
        origin: URL(string: "https://192.168.1.20:8443")!, pinnedSPKISHA256Hex: String(repeating: "ab", count: 32))

    func testNotesListModelLocalTrustChangedDeletesTokenAndSignsOut() throws {
        let store = InMemoryCredentialStore()
        try store.saveToken("saved-token", account: localConfig.keychainAccount)

        let env = AppEnvironment(credentialStore: store)
        env.configuration = localConfig
        env.sessionStore.restoreIfPossible(configuration: localConfig)
        XCTAssertEqual(env.sessionStore.state, .signedIn(localConfig))

        let model = try XCTUnwrap(env.makeNotesListModel())
        model.onLocalTrustChanged?()

        guard case .signedOut(let reason) = env.sessionStore.state else {
            return XCTFail("expected signedOut after AppEnvironment's onLocalTrustChanged wiring fires")
        }
        XCTAssertNotNil(reason)
        XCTAssertNil(store.loadToken(account: localConfig.keychainAccount))
    }

    func testNoteReaderModelLocalTrustChangedDeletesTokenAndSignsOut() throws {
        let store = InMemoryCredentialStore()
        try store.saveToken("saved-token", account: localConfig.keychainAccount)

        let env = AppEnvironment(credentialStore: store)
        env.configuration = localConfig
        env.sessionStore.restoreIfPossible(configuration: localConfig)
        XCTAssertEqual(env.sessionStore.state, .signedIn(localConfig))

        let model = try XCTUnwrap(env.makeNoteReaderModel(noteID: "n1"))
        model.onLocalTrustChanged?()

        guard case .signedOut(let reason) = env.sessionStore.state else {
            return XCTFail("expected signedOut after AppEnvironment's onLocalTrustChanged wiring fires")
        }
        XCTAssertNotNil(reason)
        XCTAssertNil(store.loadToken(account: localConfig.keychainAccount))
    }
}
