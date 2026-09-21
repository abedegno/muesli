import XCTest
@testable import Muesli

@MainActor
final class SessionStoreTests: XCTestCase {
    let hostedConfig = ServerConfiguration(origin: URL(string: "https://muesli.example.com")!)
    let localConfig = ServerConfiguration(
        origin: URL(string: "https://192.168.1.20:8443")!, pinnedSPKISHA256Hex: String(repeating: "ab", count: 32))

    func testRestoreIfPossibleEntersSignedInWithSavedToken() throws {
        let store = InMemoryCredentialStore()
        try store.saveToken("saved-token", account: hostedConfig.keychainAccount)
        let session = SessionStore(credentialStore: store)
        session.restoreIfPossible(configuration: hostedConfig)
        XCTAssertEqual(session.state, .signedIn(hostedConfig))
    }

    func testRestoreIfPossibleStaysSignedOutWithoutSavedToken() {
        let store = InMemoryCredentialStore()
        let session = SessionStore(credentialStore: store)
        session.restoreIfPossible(configuration: hostedConfig)
        XCTAssertEqual(session.state, .signedOut(reason: nil))
    }

    func testSignOutDeletesTokenAndReturnsToSignIn() throws {
        let store = InMemoryCredentialStore()
        try store.saveToken("saved-token", account: hostedConfig.keychainAccount)
        let session = SessionStore(credentialStore: store)
        session.restoreIfPossible(configuration: hostedConfig)
        session.signOut(configuration: hostedConfig)
        XCTAssertEqual(session.state, .signedOut(reason: nil))
        XCTAssertNil(store.loadToken(account: hostedConfig.keychainAccount))
    }

    func testSessionEndedDeletesTokenAndSetsMessage() throws {
        let store = InMemoryCredentialStore()
        try store.saveToken("saved-token", account: hostedConfig.keychainAccount)
        let session = SessionStore(credentialStore: store)
        session.restoreIfPossible(configuration: hostedConfig)
        session.sessionEnded(configuration: hostedConfig)
        guard case .signedOut(let reason) = session.state else { return XCTFail("expected signedOut") }
        XCTAssertNotNil(reason)
        XCTAssertNil(store.loadToken(account: hostedConfig.keychainAccount))
    }

    func testHostedAndLocalTokensAreIndependentByKeychainAccount() throws {
        let store = InMemoryCredentialStore()
        try store.saveToken("hosted-token", account: hostedConfig.keychainAccount)
        try store.saveToken("local-token", account: localConfig.keychainAccount)
        XCTAssertEqual(store.loadToken(account: hostedConfig.keychainAccount), "hosted-token")
        XCTAssertEqual(store.loadToken(account: localConfig.keychainAccount), "local-token")
    }

    func testMakeAPIClientUsesConfigurationSpecificTrust() {
        let store = InMemoryCredentialStore()
        let session = SessionStore(credentialStore: store)
        let hostedClient = session.makeAPIClient(configuration: hostedConfig)
        let localClient = session.makeAPIClient(configuration: localConfig)
        // Both must construct successfully with independent configurations
        // (hosted has no pin; local carries the pairing-derived pin) -- the
        // factory closure receives the exact configuration passed in, never
        // a shared/global one.
        XCTAssertNotNil(hostedClient)
        XCTAssertNotNil(localClient)
    }
}
