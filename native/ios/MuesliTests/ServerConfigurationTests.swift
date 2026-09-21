import XCTest
@testable import Muesli

final class ServerConfigurationTests: XCTestCase {
    func testNormalizesTrailingSlashAndWhitespace() throws {
        let url = try ServerConfigurationValidator.normalize("  https://muesli.example.com/  ")
        XCTAssertEqual(url.absoluteString, "https://muesli.example.com")
    }

    func testAcceptsExplicitPort() throws {
        let url = try ServerConfigurationValidator.normalize("https://muesli.example.com:8443")
        XCTAssertEqual(url.absoluteString, "https://muesli.example.com:8443")
    }

    func testRejectsHTTP() {
        XCTAssertThrowsError(try ServerConfigurationValidator.normalize("http://muesli.example.com")) { error in
            XCTAssertEqual(error as? ServerConfigurationError, .notHTTPS)
        }
    }

    func testRejectsPath() {
        XCTAssertThrowsError(try ServerConfigurationValidator.normalize("https://muesli.example.com/app")) { error in
            XCTAssertEqual(error as? ServerConfigurationError, .containsPath)
        }
    }

    func testRejectsQuery() {
        XCTAssertThrowsError(try ServerConfigurationValidator.normalize("https://muesli.example.com?x=1")) { error in
            XCTAssertEqual(error as? ServerConfigurationError, .containsQueryOrFragment)
        }
    }

    func testRejectsFragment() {
        XCTAssertThrowsError(try ServerConfigurationValidator.normalize("https://muesli.example.com#frag")) { error in
            XCTAssertEqual(error as? ServerConfigurationError, .containsQueryOrFragment)
        }
    }

    func testRejectsEmbeddedCredentials() {
        XCTAssertThrowsError(try ServerConfigurationValidator.normalize("https://user:pass@muesli.example.com")) { error in
            XCTAssertEqual(error as? ServerConfigurationError, .containsCredentials)
        }
    }

    func testRejectsEmpty() {
        XCTAssertThrowsError(try ServerConfigurationValidator.normalize("   ")) { error in
            XCTAssertEqual(error as? ServerConfigurationError, .empty)
        }
    }

    func testKeychainAccountIsNamespacedByOrigin() {
        let a = ServerConfiguration(origin: URL(string: "https://a.example.com")!)
        let b = ServerConfiguration(origin: URL(string: "https://b.example.com")!)
        XCTAssertNotEqual(a.keychainAccount, b.keychainAccount)
    }

    func testIsLocalReflectsPin() {
        let hosted = ServerConfiguration(origin: URL(string: "https://a.example.com")!)
        let local = ServerConfiguration(origin: URL(string: "https://192.168.1.20:8443")!, pinnedSPKISHA256Hex: "ab")
        XCTAssertFalse(hosted.isLocal)
        XCTAssertTrue(local.isLocal)
    }
}
