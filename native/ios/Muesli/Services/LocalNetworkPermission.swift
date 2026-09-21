import Foundation

/// Classifies whether a failed connection attempt to a freshly-paired local
/// origin is likely caused by iOS's "Local Network" permission being denied,
/// so PairingView can offer a real recovery path (guidance text plus a deep
/// link to the app's Settings page, per the accepted spec) instead of a
/// silent or generic-looking failure.
///
/// iOS does not expose one single, stable, documented error for this -- the
/// exact shape has varied across OS versions and connection paths, folded
/// into NSURLErrorDomain / CFNetwork / Network.framework / POSIX depending
/// on how the request was made. This checks the whole underlying-error
/// chain for the handful of signatures most consistently associated with a
/// denied Local Network prompt (in particular DNSServiceErrorType's
/// `kDNSServiceErr_PolicyDenied` == -65570, which Network.framework and
/// CFNetwork both surface for exactly this permission, and any error whose
/// description explicitly names "local network"), rather than trusting a
/// single top-level code.
///
/// This has NOT been verified against a real device/simulator with the
/// permission actually denied -- no Swift toolchain is available in the
/// sandbox this was authored in (see native/ios/README.md's environment
/// limitation). Treat this as a best-effort classifier: worst case, a
/// mis-classified error falls back to the generic connection-failure
/// message in PairingView, which is a real, working recovery path (retry)
/// on its own -- so a wrong classification degrades UX, it does not create
/// a silent failure.
public enum LocalNetworkPermission {
    /// `kDNSServiceErr_PolicyDenied`, the code Network.framework/CFNetwork
    /// use for "No permission to scan for or connect to services advertised
    /// via local networking."
    public static let deniedErrorCode = -65570

    public static func isLikelyPermissionDenied(_ error: Error) -> Bool {
        var current: Error? = error
        var hops = 0
        // Walk NSUnderlyingErrorKey rather than only inspecting the
        // top-level error: URLSession commonly wraps the real cause several
        // layers deep (NSURLErrorDomain -> kCFErrorDomainCFNetwork ->
        // NWError/POSIX), and which layer reports -65570 varies by OS
        // version. Bounded to 8 hops so a pathological cycle can't loop.
        while let err = current, hops < 8 {
            hops += 1
            let nsError = err as NSError
            if nsError.code == deniedErrorCode {
                return true
            }
            if nsError.localizedDescription.lowercased().contains("local network") {
                return true
            }
            current = nsError.userInfo[NSUnderlyingErrorKey] as? Error
        }
        return false
    }
}
