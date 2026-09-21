import Foundation

/// Shared decode-and-present logic for both of PairingView's two intake
/// paths -- manual text entry and QR scanning (issue #767) -- extracted so
/// the same behavior driving them both is directly unit-testable without
/// SwiftUI view inspection (this repo has no third-party test-only
/// dependencies, so a ViewInspector-style test isn't an option). Both paths
/// funnel into the exact same `PairingPayloadDecoder.decode`, so a payload
/// scanned as a QR code is validated identically to one pasted by hand --
/// there is no separate, weaker validation path for scanned input.
public enum PairingCodeIntake {
    public struct Outcome: Equatable {
        public let payload: PairingPayload?
        public let errorMessage: String?

        public static func decoded(_ payload: PairingPayload) -> Outcome {
            Outcome(payload: payload, errorMessage: nil)
        }

        public static func failed(_ message: String) -> Outcome {
            Outcome(payload: nil, errorMessage: message)
        }
    }

    /// The message shown for any decode failure, from either intake path.
    /// Deliberately generic (never echoes the raw payload or parser
    /// internals) since a malformed QR code and a mistyped manual entry
    /// should read the same way to the user.
    public static let invalidCodeMessage =
        "That code doesn't look right. Check Settings on your computer and try again."

    public static func process(_ raw: String) -> Outcome {
        do {
            let payload = try PairingPayloadDecoder.decode(raw)
            return .decoded(payload)
        } catch {
            return .failed(invalidCodeMessage)
        }
    }
}
