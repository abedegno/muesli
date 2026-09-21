import SwiftUI

/// Resolves which `ServerConfiguration` "Sign In" submits against (issue
/// #767 repair round). A completed local pairing pins an origin and
/// certificate fingerprint on `AppEnvironment.configuration`; that trust
/// must never be discarded in favor of a fresh, unpinned
/// `ServerConfiguration` built from Sign In's own, separately-typed (and,
/// right after pairing, still-empty) server-URL field. Extracted as a pure
/// function so this choice is unit-testable without a SwiftUI host.
enum SignInConfigurationResolver {
    static func resolve(pairedConfiguration: ServerConfiguration?, serverURLText: String) -> ServerConfiguration? {
        if let pairedConfiguration, pairedConfiguration.isLocal {
            return pairedConfiguration
        }
        guard let origin = try? ServerConfigurationValidator.normalize(serverURLText) else { return nil }
        return ServerConfiguration(origin: origin)
    }
}

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
                    // Once local pairing has pinned an origin/certificate,
                    // sign-in must submit against that exact endpoint --
                    // never a fresh, unpinned one built from this field, so
                    // the field is replaced with a read-only confirmation
                    // of what was paired instead of accepting further edits.
                    if let paired = environment.configuration, paired.isLocal {
                        LabeledContent("Paired server", value: paired.origin.absoluteString)
                            .accessibilityIdentifier("signIn.pairedServerAddress")
                    } else {
                        TextField("https://your-muesli-server.example", text: $serverURLText)
                            .textContentType(.URL)
                            .keyboardType(.URL)
                            .autocapitalization(.none)
                            .accessibilityLabel("Server URL")
                            .accessibilityIdentifier("signIn.serverURLField")
                    }
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
        guard
            let configuration = SignInConfigurationResolver.resolve(
                pairedConfiguration: environment.configuration, serverURLText: serverURLText)
        else { return }
        environment.configuration = configuration
        await environment.sessionStore.signIn(configuration: configuration, email: email, password: password)
    }
}
