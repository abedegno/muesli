import XCTest
@testable import Muesli

final class SafeMarkdownViewTests: XCTestCase {
    func testHTTPLinkIsPreserved() {
        let attributed = SafeMarkdownView.safeAttributedString(for: "[click](https://example.com)")
        let hasLink = attributed.runs.contains { $0.link?.absoluteString == "https://example.com" }
        XCTAssertTrue(hasLink)
    }

    func testJavascriptSchemeLinkIsStripped() {
        let attributed = SafeMarkdownView.safeAttributedString(for: "[click](javascript:alert(1))")
        let hasLink = attributed.runs.contains { $0.link != nil }
        XCTAssertFalse(hasLink, "a javascript: link must never remain tappable")
    }

    func testFileSchemeLinkIsStripped() {
        let attributed = SafeMarkdownView.safeAttributedString(for: "[click](file:///etc/passwd)")
        let hasLink = attributed.runs.contains { $0.link != nil }
        XCTAssertFalse(hasLink)
    }

    func testDataSchemeLinkIsStripped() {
        let attributed = SafeMarkdownView.safeAttributedString(for: "[click](data:text/html,<script>alert(1)</script>)")
        let hasLink = attributed.runs.contains { $0.link != nil }
        XCTAssertFalse(hasLink)
    }

    func testUnparseableMarkdownFallsBackToLiteralReadableText() {
        // Even a pathological input must produce readable text, never a crash.
        let raw = "not [balanced(( markdown ** here"
        let attributed = SafeMarkdownView.safeAttributedString(for: raw)
        XCTAssertFalse(String(attributed.characters).isEmpty)
    }

    func testCustomSchemeLinkIsStripped() {
        let attributed = SafeMarkdownView.safeAttributedString(for: "[open](myapp://do-something)")
        let hasLink = attributed.runs.contains { $0.link != nil }
        XCTAssertFalse(hasLink)
    }
}
