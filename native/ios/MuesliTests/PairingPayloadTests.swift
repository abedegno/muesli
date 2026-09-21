import XCTest
@testable import Muesli

final class PairingPayloadTests: XCTestCase {
    let validFingerprint = String(repeating: "ab", count: 32)

    func validJSON(origin: String = "https://192.168.1.20:8443") -> String {
        "{\"v\":1,\"origin\":\"\(origin)\",\"spki_sha256\":\"\(validFingerprint)\",\"phrase\":\"AB-CD\"}"
    }

    func testDecodesValidPayload() throws {
        let payload = try PairingPayloadDecoder.decode(validJSON())
        XCTAssertEqual(payload.origin.absoluteString, "https://192.168.1.20:8443")
        XCTAssertEqual(payload.spkiSHA256Hex, validFingerprint)
        XCTAssertEqual(payload.phrase, "AB-CD")
    }

    func testDecodesIPv6Origin() throws {
        let payload = try PairingPayloadDecoder.decode(validJSON(origin: "https://[fd12:3456:789a:1::20]:8443"))
        XCTAssertEqual(payload.origin.host, "fd12:3456:789a:1::20")
    }

    func testRejectsMalformedJSON() {
        XCTAssertThrowsError(try PairingPayloadDecoder.decode("not json"))
    }

    func testRejectsWrongVersion() {
        let json = "{\"v\":99,\"origin\":\"https://192.168.1.20:8443\",\"spki_sha256\":\"\(validFingerprint)\",\"phrase\":\"AB-CD\"}"
        XCTAssertThrowsError(try PairingPayloadDecoder.decode(json)) { error in
            XCTAssertEqual(error as? PairingPayloadError, .unsupportedVersion(99))
        }
    }

    func testRejectsHTTPOrigin() {
        XCTAssertThrowsError(try PairingPayloadDecoder.decode(validJSON(origin: "http://192.168.1.20:8443")))
    }

    func testRejectsPublicAddress() {
        XCTAssertThrowsError(try PairingPayloadDecoder.decode(validJSON(origin: "https://8.8.8.8:8443"))) { error in
            XCTAssertEqual(error as? PairingPayloadError, .originNotPrivateNetwork)
        }
    }

    func testRejectsHostname() {
        XCTAssertThrowsError(try PairingPayloadDecoder.decode(validJSON(origin: "https://example.com:8443")))
    }

    func testRejectsShortFingerprint() {
        let json = "{\"v\":1,\"origin\":\"https://192.168.1.20:8443\",\"spki_sha256\":\"abcd\",\"phrase\":\"AB-CD\"}"
        XCTAssertThrowsError(try PairingPayloadDecoder.decode(json)) { error in
            XCTAssertEqual(error as? PairingPayloadError, .invalidFingerprint)
        }
    }

    func testRejectsMissingPhrase() {
        let json = "{\"v\":1,\"origin\":\"https://192.168.1.20:8443\",\"spki_sha256\":\"\(validFingerprint)\",\"phrase\":\"\"}"
        XCTAssertThrowsError(try PairingPayloadDecoder.decode(json)) { error in
            XCTAssertEqual(error as? PairingPayloadError, .missingPhrase)
        }
    }

    func testIsPrivateNetworkLiteralRFC1918() {
        XCTAssertTrue(isPrivateNetworkLiteral("10.0.0.1"))
        XCTAssertTrue(isPrivateNetworkLiteral("172.16.0.1"))
        XCTAssertTrue(isPrivateNetworkLiteral("172.31.255.1"))
        XCTAssertTrue(isPrivateNetworkLiteral("192.168.1.1"))
        XCTAssertFalse(isPrivateNetworkLiteral("172.15.0.1"))
        XCTAssertFalse(isPrivateNetworkLiteral("172.32.0.1"))
        XCTAssertFalse(isPrivateNetworkLiteral("8.8.8.8"))
    }

    func testIsPrivateNetworkLiteralULA() {
        XCTAssertTrue(isPrivateNetworkLiteral("fd12:3456:789a:1::20"))
        XCTAssertFalse(isPrivateNetworkLiteral("2001:4860:4860::8888"))
    }
}
