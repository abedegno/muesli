import XCTest

/// UI-level smoke tests for the accepted spec's core flows (issue #767).
///
/// LIMITATION (documented per this PR's environment constraints): this
/// sandbox has no macOS/Xcode/Simulator toolchain, so these tests could not
/// be run or even compiled here. They are written against real, wired-up
/// accessibility identifiers (SignInView.swift) as a faithful skeleton for
/// the sign-in flow, and should be extended (pairing, list pagination,
/// reader, sign-out, accessibility/Dynamic Type) once run against a real
/// simulator -- see native/ios/README.md.
final class MuesliUITests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    func testSignInScreenShowsServerEmailAndPasswordFields() throws {
        let app = XCUIApplication()
        app.launch()

        XCTAssertTrue(app.textFields["signIn.serverURLField"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.textFields["signIn.emailField"].exists)
        XCTAssertTrue(app.secureTextFields["signIn.passwordField"].exists)
        XCTAssertTrue(app.buttons["signIn.submitButton"].exists)
    }

    func testSignInButtonIsDisabledWhileSigningIn() throws {
        let app = XCUIApplication()
        app.launch()

        let serverField = app.textFields["signIn.serverURLField"]
        XCTAssertTrue(serverField.waitForExistence(timeout: 5))
        serverField.tap()
        serverField.typeText("https://muesli.example.com")

        let emailField = app.textFields["signIn.emailField"]
        emailField.tap()
        emailField.typeText("user@example.com")

        let passwordField = app.secureTextFields["signIn.passwordField"]
        passwordField.tap()
        passwordField.typeText("password123")

        // Not asserting network-dependent success/failure here (no live
        // server in a UI test run without further harness support) -- only
        // that the button exists and is initially tappable.
        XCTAssertTrue(app.buttons["signIn.submitButton"].isEnabled)
    }

    func testPairingEntryPointIsReachableFromSignIn() throws {
        let app = XCUIApplication()
        app.launch()

        let pairingButton = app.buttons["signIn.pairingButton"]
        XCTAssertTrue(pairingButton.waitForExistence(timeout: 5))
        pairingButton.tap()
        XCTAssertTrue(app.navigationBars["Pair with Muesli"].waitForExistence(timeout: 5))
    }
}
