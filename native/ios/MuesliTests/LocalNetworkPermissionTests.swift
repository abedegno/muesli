import XCTest

@testable import Muesli

/// Covers `LocalNetworkPermission.isLikelyPermissionDenied`'s classification
/// of synthetic error chains shaped like the handful of signatures Apple's
/// error domains use for a denied Local Network permission (issue #767).
/// This cannot be exercised against a real denied-permission failure without
/// a device/simulator (see native/ios/README.md's environment limitation);
/// these fixtures encode the documented/widely-observed error shapes as best
/// understood without one.
final class LocalNetworkPermissionTests: XCTestCase {
    func testDetectsTheDocumentedPolicyDeniedCodeAtTheTopLevel() {
        let error = NSError(domain: "NWErrorDomain", code: LocalNetworkPermission.deniedErrorCode)

        XCTAssertTrue(LocalNetworkPermission.isLikelyPermissionDenied(error))
    }

    func testDetectsThePolicyDeniedCodeSeveralLayersDeepInNSUnderlyingError() {
        let innermost = NSError(domain: "NWErrorDomain", code: LocalNetworkPermission.deniedErrorCode)
        let middle = NSError(
            domain: "kCFErrorDomainCFNetwork",
            code: -1004,
            userInfo: [NSUnderlyingErrorKey: innermost]
        )
        let outer = NSError(
            domain: NSURLErrorDomain,
            code: NSURLErrorCannotConnectToHost,
            userInfo: [NSUnderlyingErrorKey: middle]
        )

        XCTAssertTrue(LocalNetworkPermission.isLikelyPermissionDenied(outer))
    }

    func testDetectsAnErrorWhoseDescriptionNamesLocalNetworkEvenWithoutTheCode() {
        let error = NSError(
            domain: NSURLErrorDomain,
            code: NSURLErrorCannotConnectToHost,
            userInfo: [NSLocalizedDescriptionKey: "No permission to access the Local Network."]
        )

        XCTAssertTrue(LocalNetworkPermission.isLikelyPermissionDenied(error))
    }

    func testDoesNotFlagAnUnrelatedConnectionFailure() {
        let error = NSError(
            domain: NSURLErrorDomain,
            code: NSURLErrorTimedOut,
            userInfo: [NSLocalizedDescriptionKey: "The request timed out."]
        )

        XCTAssertFalse(LocalNetworkPermission.isLikelyPermissionDenied(error))
    }

    func testDoesNotFlagAPairingTrustError() {
        // A cert-pin mismatch (APIClientError.localTrustChanged) is a
        // different, already-handled failure mode -- it must not be
        // misclassified as a Local Network permission denial.
        XCTAssertFalse(LocalNetworkPermission.isLikelyPermissionDenied(APIClientError.localTrustChanged))
    }

    func testTerminatesOnAnUnrelatedErrorChainLongerThanTheHopBound() {
        // Sanity/regression check for the 8-hop walk bound: a chain deeper
        // than that must still return promptly and correctly rather than
        // hang, even though none of these fixture nodes carry the code.
        var chain: Error = NSError(domain: "test", code: 0)
        for _ in 0..<20 {
            chain = NSError(domain: "test", code: 0, userInfo: [NSUnderlyingErrorKey: chain])
        }

        XCTAssertFalse(LocalNetworkPermission.isLikelyPermissionDenied(chain))
    }
}
