import XCTest
@testable import Muesli

@MainActor
final class NotesListModelTests: XCTestCase {
    override func setUp() {
        super.setUp()
        StubURLProtocol.reset()
    }

    func makeModel(pageSize: Int = 2) -> NotesListModel {
        let client = APIClient(
            configuration: ServerConfiguration(origin: URL(string: "https://muesli.example.com")!),
            tokenProvider: { "token" }
        ) { _ in StubURLProtocol.makeSession() }
        return NotesListModel(client: client, pageSize: pageSize)
    }

    func itemJSON(_ id: String) -> String {
        "{\"id\":\"\(id)\",\"title\":\"T\",\"status\":\"ready\",\"pinned\":false,\"created_at\":\"2026-01-01T00:00:00Z\",\"updated_at\":\"2026-01-01T00:00:00Z\",\"snippet\":\"\",\"tags\":[]}"
    }

    func testEmptyFirstPageShowsEmptyState() async {
        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[]}")
        let model = makeModel()
        await model.loadFirstPage()
        XCTAssertEqual(model.loadState, .empty)
        XCTAssertEqual(model.items, [])
    }

    func testLoadedFirstPagePopulatesItems() async {
        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[\(itemJSON("a")),\(itemJSON("b"))]}")
        let model = makeModel()
        await model.loadFirstPage()
        XCTAssertEqual(model.loadState, .loaded)
        XCTAssertEqual(model.items.map(\.id), ["a", "b"])
    }

    func testNextPageMergesByIDWithoutDuplicates() async {
        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[\(itemJSON("a"))],\"next_cursor\":\"c1\"}")
        let model = makeModel()
        await model.loadFirstPage()
        XCTAssertEqual(model.items.map(\.id), ["a"])

        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[\(itemJSON("a")),\(itemJSON("b"))]}")
        model.loadNextPageIfNeeded()
        try? await Task.sleep(nanoseconds: 200_000_000)
        XCTAssertEqual(model.items.map(\.id), ["a", "b"])
        XCTAssertFalse(model.isLoadingNextPage)
    }

    func testConcurrentNextPageCallsCoalesce() async {
        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[\(itemJSON("a"))],\"next_cursor\":\"c1\"}")
        let model = makeModel()
        await model.loadFirstPage()

        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[\(itemJSON("b"))]}")
        model.loadNextPageIfNeeded()
        model.loadNextPageIfNeeded() // should not enqueue a second request
        try? await Task.sleep(nanoseconds: 200_000_000)
        // Only one page-2 request should have been recorded (2 total: first page + one next page).
        XCTAssertEqual(StubURLProtocol.recordedRequests.count, 2)
    }

    func testFinalPageStopsRequesting() async {
        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[\(itemJSON("a"))]}") // no next_cursor
        let model = makeModel()
        await model.loadFirstPage()
        model.loadNextPageIfNeeded()
        try? await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(StubURLProtocol.recordedRequests.count, 1)
    }

    func testLaterPageFailureRetainsRowsAndOffersRetry() async {
        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[\(itemJSON("a"))],\"next_cursor\":\"c1\"}")
        let model = makeModel()
        await model.loadFirstPage()

        StubURLProtocol.enqueue(status: 500, json: "{\"error\":\"internal error\"}")
        model.loadNextPageIfNeeded()
        try? await Task.sleep(nanoseconds: 200_000_000)
        XCTAssertEqual(model.items.map(\.id), ["a"]) // retained
        XCTAssertNotNil(model.nextPageError)

        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[\(itemJSON("b"))]}")
        model.retryNextPage()
        try? await Task.sleep(nanoseconds: 200_000_000)
        XCTAssertEqual(model.items.map(\.id), ["a", "b"])
        XCTAssertNil(model.nextPageError)
    }

    func testRefreshReplacesListState() async {
        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[\(itemJSON("a")),\(itemJSON("b"))]}")
        let model = makeModel()
        await model.loadFirstPage()
        XCTAssertEqual(model.items.map(\.id), ["a", "b"])

        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[\(itemJSON("c"))]}")
        await model.refresh()
        XCTAssertEqual(model.items.map(\.id), ["c"])
    }

    func test401CallsOnSessionEndedAndDoesNotSetErrorState() async {
        StubURLProtocol.enqueue(status: 401, json: "{\"error\":\"unauthorized\"}")
        let model = makeModel()
        var sessionEndedCalled = false
        model.onSessionEnded = { sessionEndedCalled = true }
        await model.loadFirstPage()
        XCTAssertTrue(sessionEndedCalled)
        if case .error = model.loadState {
            XCTFail("401 must not surface as a generic list error state")
        }
    }
}
