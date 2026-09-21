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
    final class FakeInput {}
    final class FakeOutput {}

    final class FakeSessionPort: CaptureSessionPort {
        var canAddInputResult = true
        var canAddOutputResult = true

        private(set) var addedInputs: [FakeInput] = []
        private(set) var removedInputs: [FakeInput] = []
        private(set) var addedOutputs: [FakeOutput] = []

        func canAddInput(_ input: FakeInput) -> Bool { canAddInputResult }
        func addInput(_ input: FakeInput) { addedInputs.append(input) }
        func removeInput(_ input: FakeInput) { removedInputs.append(input) }
        func canAddOutput(_ output: FakeOutput) -> Bool { canAddOutputResult }
        func addOutput(_ output: FakeOutput) { addedOutputs.append(output) }
    }

    func testAddsBothInputAndOutputWhenBothCanBeAdded() {
        let port = FakeSessionPort()
        let input = FakeInput()
        let output = FakeOutput()

        let succeeded = configureCaptureIO(session: port, input: input, output: output)

        XCTAssertTrue(succeeded)
        XCTAssertEqual(port.addedInputs.count, 1)
        XCTAssertEqual(port.addedOutputs.count, 1)
        XCTAssertTrue(port.removedInputs.isEmpty)
    }

    func testRemovesTheAlreadyAddedInputWhenOutputCannotBeAdded() {
        // This is the failure path finding #2 flags: the device input was
        // already successfully attached (canAddInput/addInput both ran)
        // before the output attach fails. The fix must roll that input back
        // out rather than leaving an acquired capture device attached to a
        // session that never fully configured.
        let port = FakeSessionPort()
        port.canAddOutputResult = false
        let input = FakeInput()
        let output = FakeOutput()

        let succeeded = configureCaptureIO(session: port, input: input, output: output)

        XCTAssertFalse(succeeded)
        XCTAssertEqual(port.addedInputs.count, 1, "the input must have been attempted before the output")
        XCTAssertEqual(port.removedInputs.count, 1, "a failed output attach must remove the input it added")
        XCTAssertTrue(port.addedOutputs.isEmpty, "the output must never be added once canAddOutput fails")
    }

    func testAddsNeitherInputNorOutputWhenInputCannotBeAdded() {
        let port = FakeSessionPort()
        port.canAddInputResult = false
        let input = FakeInput()
        let output = FakeOutput()

        let succeeded = configureCaptureIO(session: port, input: input, output: output)

        XCTAssertFalse(succeeded)
        XCTAssertTrue(port.addedInputs.isEmpty)
        XCTAssertTrue(port.removedInputs.isEmpty)
        XCTAssertTrue(port.addedOutputs.isEmpty)
    }
}
