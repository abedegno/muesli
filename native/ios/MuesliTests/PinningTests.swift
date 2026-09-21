import XCTest
@testable import Muesli

/// Verifies PinningSessionDelegate.spkiSHA256Hex against a real,
/// independently-generated P-256 self-signed certificate (via
/// `openssl req -x509 -newkey ec ...` during development) whose SPKI
/// SHA-256 fingerprint was computed independently (via
/// `openssl x509 -pubkey | openssl pkey -pubin -outform DER` piped to
/// Python's hashlib.sha256) -- an oracle outside this codebase, exactly
/// mirroring what internal/iosaccess.spkiFingerprint computes for the same
/// key type on the Go side.
final class PinningTests: XCTestCase {
    /// DER bytes of a real ECDSA P-256 self-signed certificate, CN/SAN =
    /// 192.168.1.20.
    static let sampleCertificateDERHex =
        "308201943082013aa003020102021428d6e2168cfa6de90778ec349bacb46025275197300a06082a8648ce3d04030230173115301306035504030c0c3139322e3136382e312e3230301e170d3236303932313030323632305a170d3236303932323030323632305a30173115301306035504030c0c3139322e3136382e312e32303059301306072a8648ce3d020106082a8648ce3d03010703420004b2d23b0a7b13641b679a9d7957f9fff09327374bf509935909b23185e08ae8934a3bd70a1ff29e116e85fc63b7e6565b63c9a9ec144ff6a10fcb8bc7fa551d8da3643062301d0603551d0e0416041454f4c98e1df6c16db880f6976b2373852a99aab5301f0603551d2304183016801454f4c98e1df6c16db880f6976b2373852a99aab5300f0603551d130101ff040530030101ff300f0603551d11040830068704c0a80114300a06082a8648ce3d0403020348003045022100fe3ec6544dc6d92fbd65382edc3fbc820bfe101b2c42996c09221d69882bc50e02202b5d5869455813b1a20979df27e83abec6996ca8c19e9a7e4050f5a82f0d19b2"

    /// Independently computed: SHA-256 of the certificate's raw
    /// SubjectPublicKeyInfo DER.
    static let expectedSPKISHA256Hex = "e1ec7204e5f9a17cac02fa82f2adbce0bd78f2aedb3a08f6615c22f6efbc660c"

    static func hexToData(_ hex: String) -> Data {
        var data = Data(capacity: hex.count / 2)
        var index = hex.startIndex
        while index < hex.endIndex {
            let next = hex.index(index, offsetBy: 2)
            data.append(UInt8(hex[index..<next], radix: 16)!)
            index = next
        }
        return data
    }

    func testSPKIFingerprintMatchesIndependentOracle() {
        let der = Self.hexToData(Self.sampleCertificateDERHex)
        let fingerprint = PinningSessionDelegate.spkiSHA256Hex(fromCertificateDER: der)
        XCTAssertEqual(fingerprint, Self.expectedSPKISHA256Hex)
    }

    func testFingerprintsMatchIsCaseInsensitive() {
        XCTAssertTrue(fingerprintsMatch("ABCD", "abcd"))
        XCTAssertFalse(fingerprintsMatch("abcd", "abce"))
    }
}
