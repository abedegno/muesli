# Muesli for iOS

A dependency-free, read-only SwiftUI client for issue #767: browse your
existing notes and read their authored body and generated summaries, from
either a hosted multi-user Muesli server or a paired local Electron
instance. Note creation, audio capture, transcripts, search, and Android are
explicitly out of scope for this first slice — see the accepted spec in
issue #767 for the full product behavior this implements.

## Environment/verification status (read this first)

**This source tree was authored and unit-tested logically in a Linux sandbox
with no Xcode, no macOS, and no Swift toolchain available** -- `swift`/
`swiftc`/`xcodebuild` do not exist there, so none of the Swift code here
could be compiled, type-checked, or run locally by whoever wrote it. That is
no longer the whole story, though: this repository's CI has an `ios
(swift)` check (`.github/workflows/ci.yml`) that runs on a macOS runner with
a real Xcode/Swift toolchain, on every pull request, and it does build and
test this code for real -- `xcodebuild build`/`xcodebuild test` against an
iOS Simulator destination, both currently green (`** BUILD SUCCEEDED **`,
and every `MuesliTests` unit test passing with 0 failures -- see that job's
own log for the exact, and inevitably moving, test count rather than a
number pinned here that would just go stale as tests are added). Treat that
CI check, not this sandbox, as the source of truth for whether the Swift
here compiles and its unit tests pass.

What CI's green check does **not** yet prove, and what remains a real,
honestly-stated limitation:

- No one has run this app interactively on a real device or simulator, or
  exercised `MuesliUITests` (still not wired into any build -- see
  "Building" below), so behavioral correctness beyond what `MuesliTests`'
  unit tests assert (in particular the pairing/scan/Local-Network-denial
  UI flows) is unverified against real iOS.
- `LocalNetworkPermission`'s classifier (see "What's implemented" below) is
  a best-effort guess at iOS's actual error shape for a denied Local
  Network permission, never checked against a real device/simulator with
  the permission actually denied.

Several pieces that are easy to get subtly wrong were specifically
de-risked during development, independent of a compiler:

- The SPKI SHA-256 pinning fingerprint (`APIClient.swift`,
  `PinningSessionDelegate.spkiSHA256Hex`) uses a fixed, well-known 26-byte
  DER prefix for a P-256 `SubjectPublicKeyInfo` rather than a hand-rolled
  X.509 parser. That prefix, and the whole fingerprint derivation, were
  cross-checked against a real certificate generated with
  `openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 ...`
  during development, and `PinningTests.swift` embeds that real certificate's
  DER bytes plus an independently-computed (via `openssl` + Python's
  `hashlib`) expected fingerprint as a fixture.
- The verification phrase derivation matches
  `internal/iosaccess.VerificationPhrase` / `src/main/iosAccess/pairing.ts`'s
  algorithm exactly (base32 of the first 5 fingerprint bytes, no padding,
  upper-cased, split 4/4). `PairingPayloadTests.swift` and the TypeScript
  `pairing.test.ts` both check against a fixture value computed
  independently with Python's stdlib `base64.b32encode`.

## Building

`Package.swift` at the root of this directory is a plain-text, diffable
Swift Package Manager manifest that points its target paths directly at the
existing `Muesli/` and `MuesliTests/` directories (no restructuring into an
SPM-conventional `Sources/`/`Tests/` layout was needed). Its `Muesli`
product is declared as `.iOSApplication` -- SwiftPM's native "describe a
real, installable iOS app from a package manifest, no `.xcodeproj`
required" support -- so this directory **is** a real iOS app project, not
just a compiled library: opening it in Xcode (or running `xcodebuild`
against it, below) gives you an actual `Muesli.app` you can build, install,
and launch on a simulator or device, with no manual "create an Xcode
project and copy the sources in" step. (An earlier revision of this
manifest declared a plain `.library` product, which could not do this --
see git history if you need the prior state.)

With Xcode installed:

```bash
cd native/ios
xcodebuild build -scheme Muesli -destination 'generic/platform=iOS Simulator'
xcodebuild test -scheme Muesli -destination 'platform=iOS Simulator,name=iPhone 15'
```

`xcodebuild test` installs and launches the real app on the simulator as
the host for `MuesliTests`. Opening `native/ios` directly in Xcode (File ->
Open... on this directory or `Package.swift`) exposes the same `Muesli`
scheme for building/running/debugging interactively -- no separate
`.xcodeproj` to create or maintain.

`scripts/run-integration-tests.sh` runs an equivalent sequence
automatically when a Swift toolchain is present, and skips it with a clear
message otherwise. CI's `ios (swift)` job (`.github/workflows/ci.yml`) runs
the same two `xcodebuild` invocations above on every PR.

`MuesliUITests` (the XCUITest skeleton) is still deliberately **not** a
declared target in `Package.swift` -- XCUITest needs a real host `.app`
target and a UI Test bundle target wired to it, and SwiftPM's manifest
support has no first-class "UI test target" declaration the way an Xcode
project does (only `.iOSApplication` app products and library/executable
targets). Now that opening this directory in Xcode gives you the real
`Muesli` app/scheme, running `MuesliUITests` needs one remaining manual
step, scoped to just the UI tests:

1. In Xcode, with `native/ios` open: **File -> New -> Target -> iOS UI
   Testing Bundle**, name it `MuesliUITests`, and point its "Target to be
   Tested" at the `Muesli` app.
2. Add the existing `MuesliUITests/MuesliUITests.swift` file to that new
   target (or point the target's sources at the `MuesliUITests/` directory)
   instead of the placeholder Xcode generates.
3. Run it (⌘U with the `MuesliUITests` target selected, or via the Test
   navigator) against a simulator.

No third-party dependencies either way -- no Swift Package Manager packages
beyond this repo's own manifest, no CocoaPods. Only `Foundation`,
`SwiftUI`, `Security`, `CryptoKit`, and `AVFoundation` (all system
frameworks).

## Layout

```
Muesli/
  App/            Composition root (AppEnvironment, MuesliApp, RootView)
  Models/         ServerConfiguration, PairingPayload, PairingCodeIntake
                  (shared manual-entry/QR-scan decode path), note/summary
                  DTOs, NoteStatusPresentation (the one shared status-label
                  table)
  Services/       CredentialStore (Keychain + in-memory test double),
                  APIClient (URLSession, trust, auth, decoding),
                  LocalConnectionProbe (post-pairing reachability check),
                  LocalNetworkPermission (denial classifier)
  ViewModels/     SessionStore, NotesListModel, NoteReaderModel
  Views/          SignInView, PairingView, QRScannerView, NotesListView,
                  NoteReaderView, SafeMarkdownView (native, non-executable
                  Markdown rendering)
  Info.plist
MuesliTests/      XCTest unit tests (no network; StubURLProtocol stands in
                  for real HTTP)
MuesliUITests/    XCUITest skeleton (see the limitation note in the test
                  file itself — only the sign-in flow is covered; not yet
                  wired into a UI Test target, see "Building" above; extend
                  pairing/list/reader/accessibility coverage once it is)
```

## What's implemented vs. what needs finishing on a real device/simulator

Implemented and unit-tested (CI-verified -- see "Building" above):

- Hosted origin normalization/validation (`ServerConfigurationValidator`).
- Local pairing payload decode/validate, including private-network host
  enforcement (`PairingPayloadDecoder`).
- Keychain-backed, per-origin-namespaced credential storage plus an
  in-memory test double.
- `APIClient`: login, bounded list/detail requests, bearer attachment,
  RFC3339 decoding, uniform `{"error":"..."}` handling, and a dedicated
  certificate-pinned `URLSessionDelegate` for local connections that never
  touches hosted trust.
- `SessionStore`, `NotesListModel` (pagination, coalescing, merge-by-id,
  retained rows + retry, 401 handling), `NoteReaderModel` (no polling,
  retains partial content on non-auth failure, safe unknown-status decoding).
- `NoteStatusPresentation`: one shared label table for every status.
- `SafeMarkdownView`: native selectable text, non-HTTP(S) link schemes
  stripped, graceful literal fallback on parse failure.

Also implemented and unit-tested:

- `AVFoundation`'s `AVCaptureMetadataOutput`-based QR scanning
  (`QRScannerView.swift`), wired into `PairingView` alongside manual entry
  (the accepted spec's always-available fallback, unchanged) via the shared
  `PairingCodeIntake` decode path so a scanned code is validated exactly
  like a typed one. The input/output capture-session wiring is factored
  into a pure, generic `configureCaptureIO` function so its failure path
  (removing an already-added device input if attaching the metadata output
  fails, rather than leaking it) is unit-tested
  (`QRScannerSessionConfigurationTests.swift`) without needing a real
  capture device.
- Local-Network-permission denial guidance: `PairingView` now verifies the
  paired origin is reachable (`LocalConnectionProbe`) before saving trust,
  classifies a connection failure that looks like a denied Local Network
  permission (`LocalNetworkPermission`, best-effort — iOS exposes no single
  stable error code for this), and shows guidance text plus an
  `UIApplication.openSettingsURLString` deep link to Settings. The
  Info.plist string was already in place.

Needs finishing on a real device/simulator (see "Environment/verification
status" above):

- `LocalNetworkPermission`'s classifier is a best-effort guess at iOS's
  actual error shape for a denied Local Network permission, never checked
  against a real device/simulator with the permission actually denied.
- Wiring `MuesliUITests` into a UI Test target (see "Building" above) and
  expanding it beyond the sign-in skeleton (including the new
  scan/permission-guidance flows, which XCUITest can exercise but XCTest
  unit tests cannot).

## Mobile API contract

See [`../../docs/API.md`](../../docs/API.md)'s "Mobile API (iOS)" section for
the exact `GET /api/mobile/v1/notes` and `GET /api/mobile/v1/notes/{id}`
request/response shapes this app decodes.
