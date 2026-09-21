import Foundation

/// A minimal, unauthenticated connectivity probe against a freshly-decoded
/// local pairing origin, used only by PairingView (issue #767) to tell
/// "iOS refused to even connect" (a strong candidate for a denied Local
/// Network permission) apart from other pairing failures, before trust is
/// saved.
///
/// Deliberately independent of `APIClient`: `APIClientError.transport`
/// collapses the underlying `Error` into a `String`
/// (`error.localizedDescription`), throwing away exactly the
/// underlying-error chain `LocalNetworkPermission.isLikelyPermissionDenied`
/// needs to classify. This probe surfaces the raw `Error` instead. It
/// reuses `PinningSessionDelegate` (`APIClient.swift`) unchanged rather than
/// duplicating pinning logic.
public enum LocalConnectionProbe {
    /// Any HTTP response at all -- even a 404 -- means the TLS connection
    /// (and therefore any Local Network permission gate) succeeded, so this
    /// only cares whether the request completes, not its status code.
    public static func verify(
        origin: URL,
        pinnedSPKISHA256Hex: String,
        timeout: TimeInterval = 8,
        sessionFactory: (URLSessionDelegate) -> URLSession = { delegate in
            URLSession(configuration: .ephemeral, delegate: delegate, delegateQueue: nil)
        }
    ) async throws {
        let delegate = PinningSessionDelegate(expectedOrigin: origin, expectedSPKISHA256Hex: pinnedSPKISHA256Hex)
        let session = sessionFactory(delegate)
        defer { session.invalidateAndCancel() }
        var request = URLRequest(url: origin)
        request.httpMethod = "GET"
        request.timeoutInterval = timeout
        _ = try await session.data(for: request)
    }
}
