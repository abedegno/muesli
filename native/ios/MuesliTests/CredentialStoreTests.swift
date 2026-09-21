import XCTest
@testable import Muesli

final class CredentialStoreTests: XCTestCase {
    func testSaveLoadDeleteRoundTrip() throws {
        let store = InMemoryCredentialStore()
        XCTAssertNil(store.loadToken(account: "acct-1"))

        try store.saveToken("token-a", account: "acct-1")
        XCTAssertEqual(store.loadToken(account: "acct-1"), "token-a")

        try store.deleteToken(account: "acct-1")
        XCTAssertNil(store.loadToken(account: "acct-1"))
    }

    func testAccountsAreNamespaced() throws {
        let store = InMemoryCredentialStore()
        try store.saveToken("token-a", account: "https://a.example.com")
        try store.saveToken("token-b", account: "https://b.example.com")
        XCTAssertEqual(store.loadToken(account: "https://a.example.com"), "token-a")
        XCTAssertEqual(store.loadToken(account: "https://b.example.com"), "token-b")
        try store.deleteToken(account: "https://a.example.com")
        XCTAssertNil(store.loadToken(account: "https://a.example.com"))
        XCTAssertEqual(store.loadToken(account: "https://b.example.com"), "token-b")
    }

    func testSaveOverwritesExistingToken() throws {
        let store = InMemoryCredentialStore()
        try store.saveToken("first", account: "acct-1")
        try store.saveToken("second", account: "acct-1")
        XCTAssertEqual(store.loadToken(account: "acct-1"), "second")
    }

    func testDeleteOnAbsentAccountIsNotAnError() throws {
        let store = InMemoryCredentialStore()
        XCTAssertNoThrow(try store.deleteToken(account: "never-existed"))
    }
}
