import XCTest

@testable import Muesli

/// Covers `LocalConnectionProbe.verify`'s response/error handling using the
/// same `StubURLProtocol` test double `APIClientTests` uses, injected via
/// the probe's `sessionFactory` parameter so these tests never touch a real
/// network or a real pinning handshake (that's already covered by
/// `PinningTests.swift`).
final class LocalConnectionProbeTests: XCTestCase {
    let origin = URL(string: "https://192.168.1.20:8443")!
    let fingerprint = String(repeating: "ab", count: 32)

    override func setUp() {
        super.setUp()
        StubURLProtocol.reset()
    }

    func testSucceedsOnAnyHTTPResponseRegardlessOfStatusCode() async throws {
        // A 404 still proves the connection (and any Local Network
        // permission gate) succeeded -- verify() only cares that a response
        // came back at all.
        StubURLProtocol.enqueue(status: 404, json: "{}")

        try await LocalConnectionProbe.verify(
            origin: origin,
            pinnedSPKISHA256Hex: fingerprint,
            sessionFactory: { _ in StubURLProtocol.makeSession() }
        )
    }

    func testPropagatesATransportFailureAsTheRawError() async {
        // No stub enqueued: StubURLProtocol fails every unmatched request
        // with .unsupportedURL. Unlike APIClientError.transport(String),
        // verify() must let the raw Error through so
        // LocalNetworkPermission.isLikelyPermissionDenied can inspect it.
        do {
            try await LocalConnectionProbe.verify(
                origin: origin,
                pinnedSPKISHA256Hex: fingerprint,
                sessionFactory: { _ in StubURLProtocol.makeSession() }
            )
            XCTFail("expected verify to throw")
        } catch {
            XCTAssertTrue(error is URLError)
        }
    }
}
