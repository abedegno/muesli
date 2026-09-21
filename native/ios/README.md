# Muesli for iOS

A dependency-free, read-only SwiftUI client for issue #767: browse your
existing notes and read their authored body and generated summaries, from
either a hosted multi-user Muesli server or a paired local Electron
instance. Note creation, audio capture, transcripts, search, and Android are
explicitly out of scope for this first slice — see the accepted spec in
issue #767 for the full product behavior this implements.

## Environment limitation (read this first)

**This source tree was authored and unit-tested logically in a Linux sandbox
with no Xcode, no macOS, and no Swift toolchain available.** `swift`/`swiftc`
do not exist in that environment, so none of the Swift code here could be
compiled, type-checked, or run — not even `swift build`. Every file was
written and reviewed by careful manual reading, and several pieces that are
easy to get subtly wrong were specifically de-risked during development:

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

Treat this as a solid, carefully-reasoned first pass that **must be opened in
Xcode and built before it can be trusted** — expect to fix minor API-surface
mistakes (an availability check, an argument label) that only a real
compiler can catch. This is called out explicitly, per this task's
instructions, rather than silently presented as verified.

## Getting a real Xcode project

There is no `.xcodeproj` in this tree (also not something this sandbox could
have validated). To get building:

1. In Xcode: **File → New → Project → iOS → App**, name it `Muesli`,
   interface **SwiftUI**, language **Swift**, minimum deployment target
   **iOS 17**.
2. Delete the generated `ContentView.swift`/`MuesliApp.swift` placeholders.
3. Drag `Muesli/`, `MuesliTests/`, and `MuesliUITests/` from this directory
   into the project navigator, letting Xcode create matching groups. When
   prompted, add `MuesliTests`/`MuesliUITests` files to a new Unit
   Test/UI Test target (Xcode offers to create these automatically; add
   `XCTest` as their only dependency).
4. Replace the generated `Info.plist` with `Muesli/Info.plist` (or merge its
   keys — `NSCameraUsageDescription` and `NSLocalNetworkUsageDescription` are
   required for pairing to work).
5. No third-party dependencies — no Swift Package Manager packages, no
   CocoaPods. Only `Foundation`, `SwiftUI`, `Security`, and `CryptoKit`
   (all system frameworks).
6. Build (`⌘B`) and fix whatever a real compiler surfaces; run the unit
   tests (`⌘U`) before trusting any of this against a real server.

## Layout

```
Muesli/
  App/            Composition root (AppEnvironment, MuesliApp, RootView)
  Models/         ServerConfiguration, PairingPayload, note/summary DTOs,
                  NoteStatusPresentation (the one shared status-label table)
  Services/       CredentialStore (Keychain + in-memory test double),
                  APIClient (URLSession, trust, auth, decoding)
  ViewModels/     SessionStore, NotesListModel, NoteReaderModel
  Views/          SignInView, PairingView, NotesListView, NoteReaderView,
                  SafeMarkdownView (native, non-executable Markdown rendering)
  Info.plist
MuesliTests/      XCTest unit tests (no network; StubURLProtocol stands in
                  for real HTTP)
MuesliUITests/    XCUITest skeleton (see the limitation note in the test
                  file itself — only the sign-in flow is covered; extend
                  pairing/list/reader/accessibility coverage once this
                  builds in a real Xcode project)
```

## What's implemented vs. what needs finishing in Xcode

Implemented and unit-tested (logically, per the limitation above):

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

Needs finishing once this can actually build:

- Wiring `AVFoundation`'s `AVCaptureMetadataOutput` for real QR scanning in
  `PairingView` (currently manual-entry only, which the accepted spec
  requires anyway as the always-available fallback).
- Local-network-permission denial guidance UI (the Info.plist string is in
  place; the guided Settings deep-link UI is not yet built).
- Expanding `MuesliUITests` beyond the sign-in skeleton once there is a real
  simulator to iterate against.

## Mobile API contract

See [`../../docs/API.md`](../../docs/API.md)'s "Mobile API (iOS)" section for
the exact `GET /api/mobile/v1/notes` and `GET /api/mobile/v1/notes/{id}`
request/response shapes this app decodes.
