import XCTest

@testable import Muesli

/// Covers `configureCaptureIO`'s input/output attach-and-rollback logic
/// (`QRScannerView.swift`) -- the pure core of `ScannerViewController`'s
/// `AVCaptureSession` setup, extracted specifically so this is unit
/// testable: no real `AVCaptureSession`/capture device is available in a
/// unit test run (AVFoundation's capture types cannot be instantiated or
/// subclassed from test code), so this exercises `configureCaptureIO`
/// directly against a fake `CaptureSessionPort` instead of going through
/// `ScannerViewController`/`AVCaptureDevice.default(for:)`.
final class QRScannerSessionConfigurationTests: XCTestCase {
    /// Bare marker types standing in for `AVCaptureInput`/`AVCaptureOutput`
    /// -- `configureCaptureIO` is generic over `CaptureSessionPort`'s
    /// associated types, so these never need to be real capture objects.
    /// Each instance carries an `id` so a test can assert *which* input
    /// was removed (object identity), not just that removal happened.
    final class FakeInput {
        let id: String
        init(id: String = "input") { self.id = id }
    }
    final class FakeOutput {
        let id: String
        init(id: String = "output") { self.id = id }
    }

    /// A single, call-ordered event log -- rather than separate
    /// added/removed arrays -- so tests can assert the exact *sequence* of
    /// calls `configureCaptureIO` makes, not just how many of each
    /// happened. Separate counters would still pass a broken
    /// implementation that, say, called `removeInput` before `addInput`,
    /// or removed an input that was never added.
    enum Event: Equatable {
        case addedInput(String)
        case addedOutput(String)
        case removedInput(String)
    }

    final class FakeSessionPort: CaptureSessionPort {
        var canAddInputResult = true
        var canAddOutputResult = true
        private(set) var events: [Event] = []

        func canAddInput(_ input: FakeInput) -> Bool { canAddInputResult }
        func addInput(_ input: FakeInput) { events.append(.addedInput(input.id)) }
        func removeInput(_ input: FakeInput) { events.append(.removedInput(input.id)) }
        func canAddOutput(_ output: FakeOutput) -> Bool { canAddOutputResult }
        func addOutput(_ output: FakeOutput) { events.append(.addedOutput(output.id)) }
    }

    func testAddsBothInputAndOutputWhenBothCanBeAdded() {
        let port = FakeSessionPort()
        let input = FakeInput(id: "the-input")
        let output = FakeOutput(id: "the-output")

        let succeeded = configureCaptureIO(session: port, input: input, output: output)

        XCTAssertTrue(succeeded)
        XCTAssertEqual(port.events, [.addedInput("the-input"), .addedOutput("the-output")])
    }

    func testRemovesTheSameInputItAddedWhenOutputCannotBeAdded() {
        // This is the failure path finding #2 flags: the device input was
        // already successfully attached (canAddInput/addInput both ran)
        // before the output attach fails. The fix must roll that SAME
        // input back out, in that order, rather than leaving an acquired
        // capture device attached to a session that never fully
        // configured -- or, worse, removing something it never added. An
        // ordered event log (rather than separate add/remove counters)
        // is required to actually prove the ordering: counters alone
        // would still pass if removeInput were called before addInput, or
        // if it "removed" an input that was never added.
        let port = FakeSessionPort()
        port.canAddOutputResult = false
        let input = FakeInput(id: "the-input")
        let output = FakeOutput(id: "the-output")

        let succeeded = configureCaptureIO(session: port, input: input, output: output)

        XCTAssertFalse(succeeded)
        XCTAssertEqual(
            port.events,
            [.addedInput("the-input"), .removedInput("the-input")],
            "the input must be added, then removed (the same instance, in that order), and the output must never be added"
        )
    }

    func testAddsNeitherInputNorOutputWhenInputCannotBeAdded() {
        let port = FakeSessionPort()
        port.canAddInputResult = false
        let input = FakeInput()
        let output = FakeOutput()

        let succeeded = configureCaptureIO(session: port, input: input, output: output)

        XCTAssertFalse(succeeded)
        XCTAssertEqual(port.events, [])
    }
}
