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
            let input = try? AVCaptureDeviceInput(device: device),
            session.canAddInput(input)
        else { return }
        session.addInput(input)

        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else { return }
        session.addOutput(output)
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
