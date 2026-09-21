import SwiftUI

/// Local pairing: scan or manually enter the Electron-displayed QR payload,
/// confirm the verification phrase matches, then save the pinned
/// `ServerConfiguration` and fall through to sign-in. Contains no trust
/// logic itself -- PairingPayloadDecoder does the validation.
struct PairingView: View {
    @EnvironmentObject private var environment: AppEnvironment
    @Environment(\.dismiss) private var dismiss
    @State private var manualEntryText = ""
    @State private var decoded: PairingPayload?
    @State private var errorMessage: String?
    @State private var confirmed = false

    var body: some View {
        NavigationStack {
            Form {
                Section("Scan or paste the code shown in Muesli's Settings") {
                    TextField("Pairing code", text: $manualEntryText, axis: .vertical)
                        .accessibilityLabel("Pairing code manual entry")
                    Button("Validate") { validate() }
                }
                if let decoded {
                    Section("Verify this matches Muesli") {
                        Text(decoded.phrase)
                            .font(.title2.monospaced())
                            .accessibilityLabel("Verification phrase \(decoded.phrase)")
                        Toggle("This phrase matches what's shown on my computer", isOn: $confirmed)
                    }
                    Section {
                        Button("Save and Continue") { save(decoded) }
                            .disabled(!confirmed)
                    }
                }
                if let errorMessage {
                    Section {
                        Text(errorMessage).foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle("Pair with Muesli")
        }
    }

    private func validate() {
        do {
            decoded = try PairingPayloadDecoder.decode(manualEntryText)
            errorMessage = nil
        } catch {
            decoded = nil
            errorMessage = "That code doesn't look right. Check Settings on your computer and try again."
        }
    }

    private func save(_ payload: PairingPayload) {
        environment.configuration = ServerConfiguration(
            origin: payload.origin, pinnedSPKISHA256Hex: payload.spkiSHA256Hex)
        dismiss()
    }
}
