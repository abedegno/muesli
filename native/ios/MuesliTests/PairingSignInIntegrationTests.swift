import XCTest
@testable import Muesli

/// End-to-end proof (issue #767 repair round) that finishing local pairing
/// actually produces a usable signed-in session: pair (pins an origin +
/// certificate) -> sign in with credentials against that exact pinned
/// origin -- not a fresh, unpinned `ServerConfiguration` built from an
/// empty server-URL field -- -> an authenticated request carries the
/// resulting token to that same pinned origin. Exercises the real
/// `SignInConfigurationResolver` (what `SignInView.signIn()` calls),
/// `AppEnvironment`, `SessionStore`, and `APIClient` together, stubbed only
/// at the network boundary (`StubURLProtocol`) -- the same seam
/// `NotesListModelTests`/`SessionStoreTests` already use.
@MainActor
final class PairingSignInIntegrationTests: XCTestCase {
    let pairedConfiguration = ServerConfiguration(
        origin: URL(string: "https://192.168.1.20:8443")!, pinnedSPKISHA256Hex: String(repeating: "ab", count: 32))

    override func setUp() {
        super.setUp()
        StubURLProtocol.reset()
    }

    func testPairThenSignInThenAuthenticatedRequestUsesThePinnedOrigin() async throws {
        // 1. Pairing completed (PairingView.save's exact effect): the
        //    environment now holds the pinned local configuration, while
        //    Sign In's own text field is still empty -- nobody typed a
        //    hosted URL.
        let credentialStore = InMemoryCredentialStore()
        let env = AppEnvironment(credentialStore: credentialStore)
        env.configuration = pairedConfiguration
        let emptySignInServerURLText = ""

        // 2. SignInView.signIn()'s exact resolution logic must retain the
        //    pinned configuration rather than trying (and failing, on an
        //    empty field) to build a fresh unpinned one.
        let resolved = SignInConfigurationResolver.resolve(
            pairedConfiguration: env.configuration, serverURLText: emptySignInServerURLText)
        let configuration = try XCTUnwrap(resolved, "sign-in must retain the paired configuration")
        XCTAssertEqual(configuration, pairedConfiguration)
        XCTAssertTrue(configuration.isLocal)

        // 3. Credentials: sign in against that pinned configuration.
        StubURLProtocol.enqueue(status: 200, json: "{\"token\":\"paired-token\"}")
        let session = SessionStore(credentialStore: credentialStore) { config, token in
            APIClient(configuration: config, tokenProvider: token) { _ in StubURLProtocol.makeSession() }
        }
        await session.signIn(configuration: configuration, email: "user@example.com", password: "hunter2")
        XCTAssertEqual(session.state, .signedIn(configuration))
        XCTAssertEqual(credentialStore.loadToken(account: configuration.keychainAccount), "paired-token")

        // 4. An authenticated request from that session goes to the paired
        //    origin, carrying the bearer token -- proving the pin was never
        //    silently dropped in favor of a second, unpinned configuration.
        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[]}")
        let client = session.makeAPIClient(configuration: configuration)
        _ = try await client.listNotes(limit: 20, cursor: nil)

        let recorded = try XCTUnwrap(StubURLProtocol.recordedRequests.last)
        XCTAssertEqual(recorded.url?.host, "192.168.1.20")
        XCTAssertEqual(recorded.url?.port, 8443)
        XCTAssertEqual(recorded.value(forHTTPHeaderField: "Authorization"), "Bearer paired-token")
    }
}
