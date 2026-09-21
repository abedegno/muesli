import SwiftUI
import UIKit

/// Local pairing: scan or manually enter the Electron-displayed QR payload,
/// confirm the verification phrase matches, verify the paired origin is
/// actually reachable, then save the pinned `ServerConfiguration` and fall
/// through to sign-in. Contains no trust logic itself -- PairingPayloadDecoder
/// does the validation, PinningSessionDelegate (via LocalConnectionProbe)
/// does the handshake.
struct PairingView: View {
    @EnvironmentObject private var environment: AppEnvironment
    @Environment(\.dismiss) private var dismiss
    @State private var manualEntryText = ""
    @State private var decoded: PairingPayload?
    @State private var errorMessage: String?
    @State private var confirmed = false
    @State private var isScanning = false
    @State private var isConnecting = false
    @State private var showsLocalNetworkGuidance = false

    var body: some View {
        NavigationStack {
            Form {
                Section("Scan or paste the code shown in Muesli's Settings") {
                    Button("Scan QR Code") { isScanning = true }
                    TextField("Pairing code", text: $manualEntryText, axis: .vertical)
                        .accessibilityLabel("Pairing code manual entry")
                    Button("Validate") { intake(manualEntryText) }
                }
                if let decoded {
                    Section("Verify this matches Muesli") {
                        Text(decoded.phrase)
                            .font(.title2.monospaced())
                            .accessibilityLabel("Verification phrase \(decoded.phrase)")
                        Toggle("This phrase matches what's shown on my computer", isOn: $confirmed)
                    }
                    Section {
                        Button(isConnecting ? "Connecting…" : "Save and Continue") { save(decoded) }
                            .disabled(!confirmed || isConnecting)
                    }
                }
                if showsLocalNetworkGuidance {
                    Section("Local Network access is off") {
                        Text(
                            "Muesli needs permission to find your computer on this Wi-Fi network. "
                                + "Turn on Local Network access for Muesli in Settings, then try again."
                        )
                        Button("Open Settings") { openAppSettings() }
                    }
                }
                if let errorMessage {
                    Section {
                        Text(errorMessage).foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle("Pair with Muesli")
            .sheet(isPresented: $isScanning) {
                NavigationStack {
                    QRScannerView { code in
                        isScanning = false
                        intake(code)
                    }
                    .navigationTitle("Scan QR Code")
                    .toolbar {
                        ToolbarItem(placement: .cancellationAction) {
                            Button("Cancel") { isScanning = false }
                        }
                    }
                }
            }
        }
    }

    /// Shared handler for both the manual "Validate" button and a completed
    /// QR scan -- both funnel through `PairingCodeIntake`, so a scanned code
    /// is validated identically to a typed one.
    private func intake(_ raw: String) {
        let outcome = PairingCodeIntake.process(raw)
        decoded = outcome.payload
        errorMessage = outcome.errorMessage
        showsLocalNetworkGuidance = false
    }

    private func save(_ payload: PairingPayload) {
        isConnecting = true
        errorMessage = nil
        showsLocalNetworkGuidance = false
        Task {
            do {
                try await LocalConnectionProbe.verify(
                    origin: payload.origin, pinnedSPKISHA256Hex: payload.spkiSHA256Hex)
                await MainActor.run {
                    environment.configuration = ServerConfiguration(
                        origin: payload.origin, pinnedSPKISHA256Hex: payload.spkiSHA256Hex)
                    isConnecting = false
                    dismiss()
                }
            } catch {
                await MainActor.run {
                    isConnecting = false
                    if LocalNetworkPermission.isLikelyPermissionDenied(error) {
                        showsLocalNetworkGuidance = true
                    } else {
                        errorMessage =
                            "Could not connect to \(payload.origin.host ?? payload.origin.absoluteString)."
                            + " Check that this device and your computer are on the same Wi-Fi network."
                    }
                }
            }
        }
    }

    private func openAppSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
        UIApplication.shared.open(url)
    }
}
