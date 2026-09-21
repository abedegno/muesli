// swift-tools-version:5.9
// Package.swift for the issue #767 iOS client (native/ios/README.md).
//
// This is a real, checked-in build definition — plain text, diffable, and
// buildable by `swift build` / `swift test` without generating a binary
// .xcodeproj first. It points its target paths directly at the existing
// Muesli/ and MuesliTests/ directories rather than moving them into an
// SPM-conventional Sources/ and Tests/ layout, since nothing about this
// package's contents required that restructuring to resolve.
//
// What this does NOT give you: `MuesliUITests` is an XCUITest target that
// needs a real host .app to launch against a simulator — that requires an
// actual Xcode project/scheme (see "Getting a real Xcode project" below),
// not something plain SwiftPM can drive. It is intentionally not declared
// as a target here. `swift test` runs `MuesliTests` (pure XCTest, no UI
// host) only.
//
// This manifest itself has never been resolved or built by a real Swift
// toolchain (none is available in the sandbox this repo was authored in —
// see the "Environment limitation" section of native/ios/README.md). Treat
// it the same way as the rest of native/ios: carefully reasoned, not
// compiler-verified.
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
