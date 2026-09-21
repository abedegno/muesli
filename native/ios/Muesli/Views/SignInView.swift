import SwiftUI

/// Hosted sign-in (server URL, email, password) and an entry point into
/// local pairing (issue #767). Contains no network/trust/Keychain logic --
/// it only calls into SessionStore.
struct SignInView: View {
    @EnvironmentObject private var environment: AppEnvironment
    @State private var serverURLText = ""
    @State private var email = ""
    @State private var password = ""
    @State private var showPairing = false

    var body: some View {
        NavigationStack {
            Form {
                Section("Server") {
                    TextField("https://your-muesli-server.example", text: $serverURLText)
                        .textContentType(.URL)
                        .keyboardType(.URL)
                        .autocapitalization(.none)
                        .accessibilityLabel("Server URL")
                        .accessibilityIdentifier("signIn.serverURLField")
                }
                Section("Account") {
                    TextField("Email", text: $email)
                        .textContentType(.username)
                        .keyboardType(.emailAddress)
                        .autocapitalization(.none)
                        .accessibilityLabel("Email")
                        .accessibilityIdentifier("signIn.emailField")
                    SecureField("Password", text: $password)
                        .textContentType(.password)
                        .accessibilityLabel("Password")
                        .accessibilityIdentifier("signIn.passwordField")
                }
                if case .signedOut(let reason?) = environment.sessionStore.state {
                    Section {
                        Text(reason)
                            .foregroundStyle(.red)
                            .accessibilityLabel("Sign-in error: \(reason)")
                    }
                }
                Section {
                    Button("Sign In") { Task { await signIn() } }
                        .disabled(environment.sessionStore.state == .signingIn)
                        .accessibilityIdentifier("signIn.submitButton")
                    Button("Scan a local pairing code") { showPairing = true }
                        .accessibilityIdentifier("signIn.pairingButton")
                }
            }
            .navigationTitle("Sign In")
            .sheet(isPresented: $showPairing) {
                PairingView()
            }
        }
    }

    private func signIn() async {
        guard let origin = try? ServerConfigurationValidator.normalize(serverURLText) else { return }
        let configuration = ServerConfiguration(origin: origin)
        environment.configuration = configuration
        await environment.sessionStore.signIn(configuration: configuration, email: email, password: password)
    }
}
