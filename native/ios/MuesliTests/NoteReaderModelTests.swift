import XCTest
@testable import Muesli

@MainActor
final class NoteReaderModelTests: XCTestCase {
    override func setUp() {
        super.setUp()
        StubURLProtocol.reset()
    }

    func makeModel(noteID: String = "n1") -> NoteReaderModel {
        let client = APIClient(
            configuration: ServerConfiguration(origin: URL(string: "https://muesli.example.com")!),
            tokenProvider: { "token" }
        ) { _ in StubURLProtocol.makeSession() }
        return NoteReaderModel(client: client, noteID: noteID)
    }

    func detailJSON(status: String = "ready", summaries: String = "[]") -> String {
        "{\"note\":{\"id\":\"n1\",\"title\":\"T\",\"status\":\"\(status)\",\"pinned\":false,\"created_at\":\"2026-01-01T00:00:00Z\",\"updated_at\":\"2026-01-01T00:00:00Z\",\"tags\":[]},\"body_markdown\":\"hello\",\"summaries\":\(summaries)}"
    }

    func testLoadsDetailWithOrderedSummaries() async {
        let summaries = """
        [{"id":"s1","template_name":"A","status":"ready","truncated":false,"sections":[]},
         {"id":"s2","template_name":"B","status":"pending","truncated":false,"sections":[]}]
        """
        StubURLProtocol.enqueue(status: 200, json: detailJSON(summaries: summaries))
        let model = makeModel()
        await model.load()
        guard case .loaded(let detail) = model.state else { return XCTFail("expected loaded state") }
        XCTAssertEqual(detail.summaries.map(\.id), ["s1", "s2"])
    }

    func testEveryStatusDecodesWithoutCrashing() async {
        for status in ["draft", "recording", "uploaded", "transcribing", "summarizing", "ready", "failed", "some-future-status"] {
            StubURLProtocol.reset()
            StubURLProtocol.enqueue(status: 200, json: detailJSON(status: status))
            let model = makeModel()
            await model.load()
            guard case .loaded(let detail) = model.state else { return XCTFail("status \(status): expected loaded state") }
            XCTAssertEqual(detail.note.status, status)
        }
    }

    func testNonAuthFailureRetainsPreviouslyLoadedContent() async {
        StubURLProtocol.enqueue(status: 200, json: detailJSON())
        let model = makeModel()
        await model.load()
        guard case .loaded = model.state else { return XCTFail("expected loaded state") }

        StubURLProtocol.enqueue(status: 500, json: "{\"error\":\"internal error\"}")
        await model.refresh()
        guard case .loaded(let detail) = model.state else {
            return XCTFail("a transport/non-auth failure must retain the previously loaded detail")
        }
        XCTAssertEqual(detail.note.id, "n1")
    }

    func test404SetsNotFound() async {
        StubURLProtocol.enqueue(status: 404, json: "{\"error\":\"not found\"}")
        let model = makeModel()
        await model.load()
        XCTAssertEqual(model.state, .notFound)
    }

    func test401CallsOnSessionEnded() async {
        StubURLProtocol.enqueue(status: 401, json: "{\"error\":\"unauthorized\"}")
        let model = makeModel()
        var called = false
        model.onSessionEnded = { called = true }
        await model.load()
        XCTAssertTrue(called)
    }

    func testLocalTrustChangedCallsOnLocalTrustChanged() async {
        StubURLProtocol.enqueueFailure(URLError(.serverCertificateUntrusted))
        let model = makeModel()
        var called = false
        model.onLocalTrustChanged = { called = true }
        await model.load()
        XCTAssertTrue(called)
    }

    func testRefreshDoesNotPollOnItsOwn() async {
        // Only one request is ever made unless refresh() is called explicitly.
        StubURLProtocol.enqueue(status: 200, json: detailJSON())
        let model = makeModel()
        await model.load()
        try? await Task.sleep(nanoseconds: 300_000_000)
        XCTAssertEqual(StubURLProtocol.recordedRequests.count, 1)
    }
}
