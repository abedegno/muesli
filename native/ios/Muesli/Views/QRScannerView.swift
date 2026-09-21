import AVFoundation
import SwiftUI
import UIKit

/// A thin SwiftUI wrapper around AVFoundation's metadata-output QR scanning
/// (issue #767), feeding a decoded QR payload string through the exact same
/// `PairingPayloadDecoder`/`PairingCodeIntake` manual entry uses -- there is
/// no separate, weaker validation path for scanned input. Manual entry
/// remains available in `PairingView` as the spec-required always-available
/// fallback; this is purely an additional intake path.
///
/// Camera access itself is gated by `NSCameraUsageDescription` (already
/// present in `Info.plist`); this view does not request or check that
/// permission directly -- `AVCaptureSession.startRunning()` simply produces
/// no frames if it was denied, so a user who denies Camera access sees an
/// inert preview with the "Pairing code" manual-entry fallback still
/// available, which is not a silent failure. (The permission-denial
/// guidance this task adds is specifically for the *Local Network*
/// permission, gating the pairing *connection* the app makes after a code
/// is accepted, not the Camera permission gating scanning it.)
struct QRScannerView: UIViewControllerRepresentable {
    let onCode: (String) -> Void

    func makeUIViewController(context: Context) -> ScannerViewController {
        let controller = ScannerViewController()
        controller.onCode = onCode
        return controller
    }

    func updateUIViewController(_ uiViewController: ScannerViewController, context: Context) {}
}

/// A minimal seam over the "add input, then add output" half of
/// `AVCaptureSession`'s setup surface, so `configureCaptureIO` below can be
/// exercised by a unit test without a real capture device or session --
/// AVFoundation's own capture types cannot be instantiated or subclassed
/// from test code, and no capture hardware is available in CI/unit test
/// runs. `AVCaptureSession` itself conforms via `RealCaptureSessionPort`.
protocol CaptureSessionPort {
    associatedtype Input
    associatedtype Output
    func canAddInput(_ input: Input) -> Bool
    func addInput(_ input: Input)
    func removeInput(_ input: Input)
    func canAddOutput(_ output: Output) -> Bool
    func addOutput(_ output: Output)
}

/// Adapts a real `AVCaptureSession` to `CaptureSessionPort` by plain
/// delegation (no subclassing of AVFoundation types, which Apple does not
/// support from third-party code).
struct RealCaptureSessionPort: CaptureSessionPort {
    let session: AVCaptureSession

    func canAddInput(_ input: AVCaptureInput) -> Bool { session.canAddInput(input) }
    func addInput(_ input: AVCaptureInput) { session.addInput(input) }
    func removeInput(_ input: AVCaptureInput) { session.removeInput(input) }
    func canAddOutput(_ output: AVCaptureOutput) -> Bool { session.canAddOutput(output) }
    func addOutput(_ output: AVCaptureOutput) { session.addOutput(output) }
}

/// Pure, testable core of the capture-session wiring `configureSession()`
/// performs: attempts to add `input`, then `output`, rolling the input back
/// out with `removeInput` if adding the output fails -- so a successfully
/// acquired capture device is never left attached to a session it could not
/// be fully wired into. Returns whether both were added.
///
/// Generic over `CaptureSessionPort` purely so this logic (specifically the
/// failure-path rollback) can be unit-tested against a fake session/input/
/// output in `QRScannerSessionConfigurationTests.swift`, without a real
/// `AVCaptureSession` or capture device.
@discardableResult
func configureCaptureIO<Session: CaptureSessionPort>(
    session: Session,
    input: Session.Input,
    output: Session.Output
) -> Bool {
    guard session.canAddInput(input) else { return false }
    session.addInput(input)

    guard session.canAddOutput(output) else {
        session.removeInput(input)
        return false
    }
    session.addOutput(output)
    return true
}

/// Owns the `AVCaptureSession` lifecycle. A plain `UIViewController` (not a
/// SwiftUI-native type) because `AVCaptureVideoPreviewLayer` needs a real
/// `CALayer`-backed view to attach to.
final class ScannerViewController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onCode: ((String) -> Void)?

    private let session = AVCaptureSession()
    private var previewLayer: AVCaptureVideoPreviewLayer?
    /// Metadata output can fire repeatedly per frame while a code stays in
    /// frame; only the first successfully-read code is forwarded so the
    /// caller (PairingView) doesn't see the same scan several times.
    private var didEmit = false

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        configureSession()
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        previewLayer?.frame = view.bounds
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        guard !session.isRunning else { return }
        let session = self.session
        DispatchQueue.global(qos: .userInitiated).async {
            session.startRunning()
        }
    }

    override func viewDidDisappear(_ animated: Bool) {
        super.viewDidDisappear(animated)
        guard session.isRunning else { return }
        let session = self.session
        DispatchQueue.global(qos: .userInitiated).async {
            session.stopRunning()
        }
    }

    private func configureSession() {
        guard
            let device = AVCaptureDevice.default(for: .video),
            let input = try? AVCaptureDeviceInput(device: device)
        else { return }

        let output = AVCaptureMetadataOutput()
        guard configureCaptureIO(session: RealCaptureSessionPort(session: session), input: input, output: output)
        else { return }

        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]

        let layer = AVCaptureVideoPreviewLayer(session: session)
        layer.frame = view.bounds
        layer.videoGravity = .resizeAspectFill
        view.layer.addSublayer(layer)
        previewLayer = layer
    }

    func metadataOutput(
        _ output: AVCaptureMetadataOutput,
        didOutput metadataObjects: [AVMetadataObject],
        from connection: AVCaptureConnection
    ) {
        guard !didEmit,
            let object = metadataObjects.first as? AVMetadataMachineReadableCodeObject,
            object.type == .qr,
            let value = object.stringValue
        else { return }
        didEmit = true
        onCode?(value)
    }
}
