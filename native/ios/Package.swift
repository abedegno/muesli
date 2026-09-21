// swift-tools-version:5.9
// Package.swift for the issue #767 iOS client (native/ios/README.md).
//
// This is a real, checked-in build definition -- plain text, diffable, and
// does not need a binary .xcodeproj to build (see the CI note below). It
// points its target paths directly at the existing Muesli/ and
// MuesliTests/ directories rather than moving them into an SPM-conventional
// Sources/ and Tests/ layout, since nothing about this package's contents
// required that restructuring to resolve.
//
// The `Muesli` product below is an `.iOSApplication` -- SwiftPM's native
// "describe a real installable iOS app entirely from a package manifest,
// no .xcodeproj required" support (added in swift-tools-version 5.7).
// Xcode and `xcodebuild` treat a package whose manifest declares this
// product type as a genuine app project: opening this directory in Xcode
// (or `xcodebuild build/test -scheme Muesli -destination '...iOS
// Simulator...'`, exactly what CI's `ios (swift)` job in
// `.github/workflows/ci.yml` already runs) produces a real `.app` bundle
// and, for `test`, installs and launches that app on a simulator as the
// host app for `MuesliTests` -- something a plain `.library` product
// (what this manifest declared before this fix) can never do. No CI
// workflow change was needed for this: the existing `xcodebuild` commands
// already target a generic `iOS Simulator` destination with an
// auto-discovered `Muesli` scheme, which is exactly what an
// `.iOSApplication` product also produces.
//
// The `Muesli` target is correspondingly an `.executableTarget` rather than
// a `.target`: an `.iOSApplication` product's target must contain the
// app's entry point (`@main struct MuesliApp: App`, in
// `Muesli/App/MuesliApp.swift`). This does not break `MuesliTests`'
// `@testable import Muesli` -- a Swift executable target that defines its
// entry point via the `@main` attribute (rather than a bare `main.swift`
// script, which this target does not have) is importable like any other
// module, exactly as it already was as a library target.
//
// `additionalInfoPlistContentFilePath` points at the pre-existing
// `Muesli/Info.plist` (still excluded below from the target's compiled
// Swift sources, since it's plist content) so its
// `NSCameraUsageDescription`/`NSLocalNetworkUsageDescription` strings (and
// the ATS posture documented in that file) are merged into the Info.plist
// the `.iOSApplication` product generates from the manifest fields below,
// rather than needing to be duplicated into this file.
//
// What this still does NOT give you: `MuesliUITests` is an XCUITest target
// that needs a real host `.app` to launch against a simulator. That `.app`
// now exists (see above), but SwiftPM's manifest support has no
// first-class "UI test target" declaration the way an Xcode project does,
// so `MuesliUITests` is still intentionally not declared as a target here
// -- see "Building" in native/ios/README.md for how to run it against the
// app this manifest now produces.
//
// This manifest was authored (and this fix written) without a Swift
// toolchain available in the sandbox this repo was originally written in
// (see the "Environment limitation" section of native/ios/README.md), so
// the `.iOSApplication` product type/parameter names used below could not
// be compiler-verified locally -- but CI's "ios (swift)" job
// (.github/workflows/ci.yml) builds and tests it for real, against an iOS
// Simulator destination via `xcodebuild`, on a macOS runner for every PR,
// and will fail loudly if anything here doesn't match SwiftPM's actual API.
import PackageDescription

let package = Package(
    name: "Muesli",
    platforms: [
        .iOS(.v17)
    ],
    products: [
        .iOSApplication(
            name: "Muesli",
            targets: ["Muesli"],
            bundleIdentifier: "org.muesli.ios",
            displayVersion: "1.0",
            bundleVersion: "1",
            supportedDeviceFamilies: [.phone],
            supportedInterfaceOrientations: [
                .portrait,
                .landscapeLeft,
                .landscapeRight,
            ],
            additionalInfoPlistContentFilePath: "Muesli/Info.plist"
        )
    ],
    targets: [
        .executableTarget(
            name: "Muesli",
            path: "Muesli",
            exclude: ["Info.plist"]
        ),
        .testTarget(
            name: "MuesliTests",
            dependencies: ["Muesli"],
            path: "MuesliTests"
        ),
    ]
)
