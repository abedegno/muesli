// swift-tools-version:5.9
// Package.swift for the issue #767 iOS client (native/ios/README.md).
//
// This is a real, checked-in build definition — plain text, diffable, and
// does not need a binary .xcodeproj to build (see the CI note below). It
// points its target paths directly at the existing Muesli/ and
// MuesliTests/ directories rather than moving them into an SPM-conventional
// Sources/ and Tests/ layout, since nothing about this package's contents
// required that restructuring to resolve.
//
// The Muesli target's Views import UIKit (this is an iOS app), so plain
// `swift build` / `swift test` — which target the host platform (macOS) by
// default, and macOS has no UIKit — cannot build this package: it needs an
// iOS Simulator destination, e.g.
// `xcodebuild build -scheme Muesli -destination 'generic/platform=iOS Simulator'`
// (see "Building" in native/ios/README.md).
//
// What this does NOT give you: `MuesliUITests` is an XCUITest target that
// needs a real host .app to launch against a simulator — that requires an
// actual Xcode project/scheme (see "Getting a real Xcode project" below),
// not something SwiftPM/xcodebuild-against-the-package can drive. It is
// intentionally not declared as a target here. `xcodebuild test` against
// this package's scheme runs `MuesliTests` (pure XCTest, no UI host) only.
//
// This manifest was authored without a Swift toolchain available in the
// sandbox this repo was originally written in (see the "Environment
// limitation" section of native/ios/README.md), but it is no longer
// compiler-unverified: CI's "ios (swift)" job (.github/workflows/ci.yml)
// builds and tests it for real, against an iOS Simulator destination via
// `xcodebuild`, on a macOS runner for every PR.
import PackageDescription

let package = Package(
    name: "Muesli",
    platforms: [
        .iOS(.v17),
    ],
    products: [
        .library(name: "Muesli", targets: ["Muesli"]),
    ],
    targets: [
        .target(
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
