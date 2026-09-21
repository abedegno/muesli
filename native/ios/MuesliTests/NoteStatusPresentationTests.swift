import XCTest
@testable import Muesli

final class NoteStatusPresentationTests: XCTestCase {
    func testAllKnownStatusesHaveDistinctLabelsAndAreKnown() {
        let statuses = ["draft", "recording", "uploaded", "transcribing", "summarizing", "ready", "failed"]
        var labels = Set<String>()
        for status in statuses {
            XCTAssertTrue(NoteStatusPresentation.isKnown(status), status)
            let label = NoteStatusPresentation.label(for: status)
            XCTAssertNotEqual(label, "Unknown", status)
            labels.insert(label)
        }
        XCTAssertEqual(labels.count, statuses.count, "expected distinct labels for every known status")
    }

    func testUnknownStatusDecodesSafely() {
        XCTAssertFalse(NoteStatusPresentation.isKnown("some-future-status"))
        XCTAssertEqual(NoteStatusPresentation.label(for: "some-future-status"), "Unknown")
    }

    func testNonterminalStatusesGetAReadinessBanner() {
        for status in NoteStatusPresentation.nonterminalStatuses {
            XCTAssertNotNil(NoteStatusPresentation.readinessBannerText(for: status))
            XCTAssertTrue(NoteStatusPresentation.readinessBannerText(for: status)!.contains(NoteStatusPresentation.label(for: status)))
        }
    }

    func testReadyHasNoReadinessBanner() {
        XCTAssertNil(NoteStatusPresentation.readinessBannerText(for: "ready"))
    }

    func testFailedHasNoReadinessBannerButIsFailed() {
        XCTAssertNil(NoteStatusPresentation.readinessBannerText(for: "failed"))
        XCTAssertTrue(NoteStatusPresentation.isFailed("failed"))
    }
}
