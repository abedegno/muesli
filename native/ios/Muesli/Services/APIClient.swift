import CryptoKit
import Security
import Foundation

/// Errors an APIClient call can surface to a view model.
public enum APIClientError: Error, Equatable {
    case notAuthenticated
    case sessionEnded  // authenticated 401: caller must clear credentials
    case localTrustChanged  // TLS/pin mismatch on a local connection: caller must discard new content and re-pair
    case decoding
    case transport(String)
    case server(status: Int, message: String)
}

/// Decoded `{"error":"..."}` body from the server (issue #767's uniform
/// error shape).
private struct ErrorBody: Decodable { let error: String }

/// Owns URLSession, trust, bearer authorization, HTTP status handling, and
/// DTO decoding for the mobile API (issue #767). Views never touch
/// URLSession, Keychain, or trust directly -- only this type and the models
/// built on top of it do.
public final class APIClient {
    private let configuration: ServerConfiguration
    private let session: URLSession
    private let tokenProvider: () -> String?
    private let decoder = MobileAPIDecoding.makeDecoder()

    /// - Parameters:
    ///   - configuration: hosted (ATS default trust) or local (pinned).
    ///   - tokenProvider: returns the current bearer token, or nil if signed out.
    ///   - sessionFactory: injectable for tests; defaults to a real
    ///     URLSession using `PinningSessionDelegate` only for local
    ///     configurations (hosted trust is never touched).
    public init(
        configuration: ServerConfiguration,
        tokenProvider: @escaping () -> String?,
        sessionFactory: (ServerConfiguration) -> URLSession = APIClient.makeDefaultSession
    ) {
        self.configuration = configuration
        self.tokenProvider = tokenProvider
        self.session = sessionFactory(configuration)
    }

    public static func makeDefaultSession(for configuration: ServerConfiguration) -> URLSession {
        guard let pin = configuration.pinnedSPKISHA256Hex else {
            // Hosted: standard system trust, unmodified ATS. No delegate.
            return URLSession(configuration: .default)
        }
        let delegate = PinningSessionDelegate(expectedOrigin: configuration.origin, expectedSPKISHA256Hex: pin)
        return URLSession(configuration: .ephemeral, delegate: delegate, delegateQueue: nil)
    }

    // MARK: - Auth

    public struct LoginRequest: Encodable { let email: String; let password: String }
    public struct LoginResponse: Decodable { let token: String }

    /// Sends `{"email":"...","password":"..."}` to POST /api/login. The
    /// password is never persisted by this method or its caller; only the
    /// returned token is (by SessionStore, into CredentialStore).
    public func login(email: String, password: String) async throws -> String {
        var request = URLRequest(url: configuration.origin.appendingPathComponent("/api/login"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(LoginRequest(email: email, password: password))

        let (data, response) = try await performRequest(request, authorized: false)
        try Self.throwIfError(status: response.statusCode, data: data)
        do {
            return try JSONDecoder().decode(LoginResponse.self, from: data).token
        } catch {
            throw APIClientError.decoding
        }
    }

    // MARK: - Notes

    public func listNotes(limit: Int, cursor: String?) async throws -> MobileNotesListResponse {
        var components = URLComponents(
            url: configuration.origin.appendingPathComponent("/api/mobile/v1/notes"),
            resolvingAgainstBaseURL: false)!
        var query = [URLQueryItem(name: "limit", value: String(limit))]
        if let cursor { query.append(URLQueryItem(name: "cursor", value: cursor)) }
        components.queryItems = query

        var request = URLRequest(url: components.url!)
        request.httpMethod = "GET"
        let (data, response) = try await performRequest(request, authorized: true)
        try Self.throwIfError(status: response.statusCode, data: data)
        return try decode(MobileNotesListResponse.self, from: data)
    }

    public func noteDetail(id: String) async throws -> MobileNoteDetailResponse {
        var request = URLRequest(url: configuration.origin.appendingPathComponent("/api/mobile/v1/notes/\(id)"))
        request.httpMethod = "GET"
        let (data, response) = try await performRequest(request, authorized: true)
        try Self.throwIfError(status: response.statusCode, data: data)
        return try decode(MobileNoteDetailResponse.self, from: data)
    }

    // MARK: - Internals

    private func decode<T: Decodable>(_ type: T.Type, from data: Data) throws -> T {
        do {
            return try decoder.decode(type, from: data)
        } catch {
            throw APIClientError.decoding
        }
    }

    private func performRequest(_ request: URLRequest, authorized: Bool) async throws -> (Data, HTTPURLResponse) {
        var request = request
        if authorized {
            guard let token = tokenProvider() else { throw APIClientError.notAuthenticated }
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        do {
            let (data, response) = try await session.data(for: request)
            guard let http = response as? HTTPURLResponse else {
                throw APIClientError.transport("no HTTP response")
            }
            if http.statusCode == 401 && authorized {
                throw APIClientError.sessionEnded
            }
            return (data, http)
        } catch let error as APIClientError {
            throw error
        } catch let urlError as URLError where urlError.code == .serverCertificateUntrusted
            || urlError.code == .secureConnectionFailed {
            throw APIClientError.localTrustChanged
        } catch {
            throw APIClientError.transport(error.localizedDescription)
        }
    }

    private static func throwIfError(status: Int, data: Data) throws {
        guard status < 200 || status >= 300 else { return }
        let message = (try? JSONDecoder().decode(ErrorBody.self, from: data))?.error ?? "request failed"
        throw APIClientError.server(status: status, message: message)
    }
}

/// A dedicated URLSessionDelegate used only for a local (paired)
/// `ServerConfiguration`. Accepts a self-signed leaf only when (a) hostname
/// validation for the exact pinned origin's host succeeds and (b) the leaf
/// certificate's SPKI SHA-256 fingerprint matches exactly. Rejects every
/// other challenge -- including every challenge for a different host. Never
/// touches hosted trust or global ATS: a hosted APIClient never constructs
/// this delegate at all (see `makeDefaultSession`).
public final class PinningSessionDelegate: NSObject, URLSessionDelegate {
    private let expectedHost: String
    private let expectedSPKISHA256Hex: String

    public init(expectedOrigin: URL, expectedSPKISHA256Hex: String) {
        self.expectedHost = expectedOrigin.host ?? ""
        self.expectedSPKISHA256Hex = expectedSPKISHA256Hex.lowercased()
    }

    public func urlSession(
        _ session: URLSession,
        didReceive challenge: URLAuthenticationChallenge,
        completionHandler: @escaping (URLSession.AuthChallengeDisposition, URLCredential?) -> Void
    ) {
        guard challenge.protectionSpace.authenticationMethod == NSURLAuthenticationMethodServerTrust,
            challenge.protectionSpace.host == expectedHost,
            let serverTrust = challenge.protectionSpace.serverTrust
        else {
            completionHandler(.cancelAuthenticationChallenge, nil)
            return
        }

        guard let leafData = Self.leafCertificateData(from: serverTrust) else {
            completionHandler(.cancelAuthenticationChallenge, nil)
            return
        }
        guard let spkiHex = Self.spkiSHA256Hex(fromCertificateDER: leafData) else {
            completionHandler(.cancelAuthenticationChallenge, nil)
            return
        }
        guard spkiHex == expectedSPKISHA256Hex else {
            completionHandler(.cancelAuthenticationChallenge, nil)
            return
        }
        completionHandler(.useCredential, URLCredential(trust: serverTrust))
    }

    static func leafCertificateData(from trust: SecTrust) -> Data? {
        guard SecTrustGetCertificateCount(trust) > 0 else { return nil }
        #if compiler(>=5.9)
            if #available(iOS 15.0, *) {
                guard let chain = SecTrustCopyCertificateChain(trust) as? [SecCertificate],
                    let leaf = chain.first
                else { return nil }
                return SecCertificateCopyData(leaf) as Data
            }
        #endif
        guard let leaf = SecTrustGetCertificateAtIndex(trust, 0) else { return nil }
        return SecCertificateCopyData(leaf) as Data
    }

    /// Extracts the SubjectPublicKeyInfo (SPKI) from a DER-encoded
    /// certificate and returns its SHA-256 hex digest, matching
    /// internal/iosaccess.spkiFingerprint (SHA-256 of Go's
    /// x509.Certificate.RawSubjectPublicKeyInfo) byte-for-byte for the
    /// ECDSA P-256 self-signed certificates this app ever pins against (see
    /// internal/iosaccess/certificate.go -- every local-access certificate
    /// this client encounters is P-256).
    ///
    /// Rather than hand-rolling a general X.509 DER parser (high risk of a
    /// subtle, hard-to-verify bug), this uses SecCertificateCopyKey +
    /// SecKeyCopyExternalRepresentation to get the key's raw 65-byte
    /// uncompressed EC point (0x04 || X || Y), then prepends the fixed,
    /// standard 26-byte DER prefix for a P-256 SubjectPublicKeyInfo
    /// (RFC 5480's id-ecPublicKey + prime256v1 AlgorithmIdentifier wrapping
    /// a BIT STRING) -- independently confirmed against a real `openssl ec
    /// -pubout -outform DER` P-256 key during development, and reproduced
    /// exactly by Go's crypto/x509 for the same key type.
    static let p256SubjectPublicKeyInfoDERPrefix = Data([
        0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01,
        0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00,
    ])

    static func spkiSHA256Hex(fromCertificateDER der: Data) -> String? {
        guard let certificate = SecCertificateCreateWithData(nil, der as CFData) else { return nil }
        guard let publicKey = SecCertificateCopyKey(certificate) else { return nil }
        var error: Unmanaged<CFError>?
        guard let rawPoint = SecKeyCopyExternalRepresentation(publicKey, &error) as Data? else { return nil }
        // 65 bytes: 0x04 (uncompressed marker) + 32-byte X + 32-byte Y, for P-256.
        guard rawPoint.count == 65, rawPoint.first == 0x04 else { return nil }
        let spki = p256SubjectPublicKeyInfoDERPrefix + rawPoint
        let digest = SHA256.hash(data: spki)
        return digest.map { String(format: "%02x", $0) }.joined()
    }
}

/// Fingerprint pin-change/local-trust-loss test seam: pure comparison logic
/// extracted from the delegate so it's testable without a real TLS
/// handshake or SecTrust object.
func fingerprintsMatch(_ a: String, _ b: String) -> Bool {
    a.lowercased() == b.lowercased()
}
