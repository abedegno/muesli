import XCTest

@testable import Muesli

/// Covers PairingView's shared manual-entry/QR-scan intake path (issue
/// #767): both `PairingView.intake(_:)` call sites funnel through
/// `PairingCodeIntake.process`, so this is the directly-testable proof that
/// a scanned QR payload is validated exactly like a typed one -- there is no
/// separate, weaker path for scanner input.
final class PairingCodeIntakeTests: XCTestCase {
    let validFingerprint = String(repeating: "ab", count: 32)

    func validJSON(origin: String = "https://192.168.1.20:8443") -> String {
        "{\"v\":1,\"origin\":\"\(origin)\",\"spki_sha256\":\"\(validFingerprint)\",\"phrase\":\"AB-CD\"}"
    }

    func testProcessesAValidPayloadTheSameWayRegardlessOfIntakeSource() {
        let raw = validJSON()

        // Simulates both intake paths handing PairingCodeIntake the exact
        // same raw string -- a QR scan and manual entry are indistinguishable
        // once decoded, which is the property this test protects.
        let fromManualEntry = PairingCodeIntake.process(raw)
        let fromScan = PairingCodeIntake.process(raw)

        XCTAssertEqual(fromManualEntry, fromScan)
        XCTAssertNil(fromManualEntry.errorMessage)
        XCTAssertEqual(fromManualEntry.payload?.phrase, "AB-CD")
        XCTAssertEqual(fromManualEntry.payload?.spkiSHA256Hex, validFingerprint)
    }

    func testFailsClosedOnAScannedPayloadThatIsNotValidJSON() {
        // A QR code can contain arbitrary text (a URL, a Wi-Fi config, junk);
        // scanning something that isn't a Muesli pairing payload must not
        // crash or produce a partially-trusted result.
        let outcome = PairingCodeIntake.process("https://example.com/not-a-pairing-code")

        XCTAssertNil(outcome.payload)
        XCTAssertEqual(outcome.errorMessage, PairingCodeIntake.invalidCodeMessage)
    }

    func testFailsClosedOnAScannedPublicOrigin() {
        // Regression guard: a QR code claiming a public-internet origin must
        // be rejected the same way PairingPayloadDecoder already rejects it
        // for manual entry (see PairingPayloadTests.testRejectsPublicAddress).
        let outcome = PairingCodeIntake.process(validJSON(origin: "https://8.8.8.8:8443"))

        XCTAssertNil(outcome.payload)
        XCTAssertEqual(outcome.errorMessage, PairingCodeIntake.invalidCodeMessage)
    }

    func testGenericErrorMessageNeverEchoesTheRawInput() {
        let raw = "some-random-scanned-junk-\(UUID().uuidString)"
        let outcome = PairingCodeIntake.process(raw)

        XCTAssertFalse((outcome.errorMessage ?? "").contains(raw))
    }
}
