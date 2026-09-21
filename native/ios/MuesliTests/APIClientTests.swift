import XCTest
@testable import Muesli

final class APIClientTests: XCTestCase {
    override func setUp() {
        super.setUp()
        StubURLProtocol.reset()
    }

    func makeClient(configuration: ServerConfiguration = ServerConfiguration(origin: URL(string: "https://muesli.example.com")!), token: String? = "test-token") -> APIClient {
        APIClient(configuration: configuration, tokenProvider: { token }) { _ in StubURLProtocol.makeSession() }
    }

    func testLoginSendsExactShapeAndReturnsToken() async throws {
        StubURLProtocol.enqueue(status: 200, json: "{\"token\":\"abc123\"}")
        let client = makeClient()
        let token = try await client.login(email: "user@example.com", password: "s3cret")
        XCTAssertEqual(token, "abc123")

        let request = StubURLProtocol.recordedRequests.last!
        XCTAssertEqual(request.url?.path, "/api/login")
        XCTAssertEqual(request.httpMethod, "POST")
        let body = try JSONSerialization.jsonObject(with: request.httpBodyDataForTest()!) as! [String: String]
        XCTAssertEqual(body, ["email": "user@example.com", "password": "s3cret"])
    }

    func testLoginDoesNotAttachBearerToken() async throws {
        StubURLProtocol.enqueue(status: 200, json: "{\"token\":\"abc123\"}")
        let client = makeClient(token: "should-not-be-sent")
        _ = try await client.login(email: "user@example.com", password: "s3cret")
        let request = StubURLProtocol.recordedRequests.last!
        XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"))
    }

    func testListNotesAttachesBearerToken() async throws {
        StubURLProtocol.enqueue(status: 200, json: "{\"items\":[]}")
        let client = makeClient(token: "bearer-xyz")
        _ = try await client.listNotes(limit: 30, cursor: nil)
        let request = StubURLProtocol.recordedRequests.last!
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer bearer-xyz")
    }

    func testListNotesDecodesRealResponseShape() async throws {
        let json = """
        {"items":[{"id":"n1","title":"T","status":"ready","pinned":true,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","snippet":"s","tags":["a","b"]}],"next_cursor":"abc"}
        """
        StubURLProtocol.enqueue(status: 200, json: json)
        let client = makeClient()
        let page = try await client.listNotes(limit: 30, cursor: nil)
        XCTAssertEqual(page.items.count, 1)
        XCTAssertEqual(page.items[0].id, "n1")
        XCTAssertEqual(page.items[0].tags, ["a", "b"])
        XCTAssertEqual(page.nextCursor, "abc")
    }

    func testNoteDetailDecodesRealResponseShape() async throws {
        let json = """
        {"note":{"id":"n1","title":"T","status":"ready","pinned":false,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","tags":[]},"body_markdown":"hello","summaries":[{"id":"s1","template_name":"Recap","status":"ready","truncated":false,"sections":[{"heading":"H","content_markdown":"C"}]}]}
        """
        StubURLProtocol.enqueue(status: 200, json: json)
        let client = makeClient()
        let detail = try await client.noteDetail(id: "n1")
        XCTAssertEqual(detail.note.id, "n1")
        XCTAssertEqual(detail.bodyMarkdown, "hello")
        XCTAssertEqual(detail.summaries.count, 1)
        XCTAssertEqual(detail.summaries[0].sections[0].heading, "H")
    }

    func testWithoutTokenThrowsNotAuthenticated() async {
        let client = makeClient(token: nil)
        do {
            _ = try await client.listNotes(limit: 30, cursor: nil)
            XCTFail("expected an error")
        } catch let error as APIClientError {
            XCTAssertEqual(error, .notAuthenticated)
        } catch {
            XCTFail("wrong error type: \(error)")
        }
    }

    func test401ThrowsSessionEnded() async {
        StubURLProtocol.enqueue(status: 401, json: "{\"error\":\"unauthorized\"}")
        let client = makeClient()
        do {
            _ = try await client.listNotes(limit: 30, cursor: nil)
            XCTFail("expected an error")
        } catch let error as APIClientError {
            XCTAssertEqual(error, .sessionEnded)
        } catch {
            XCTFail("wrong error type: \(error)")
        }
    }

    func testCertificateErrorThrowsLocalTrustChanged() async {
        StubURLProtocol.enqueueFailure(URLError(.serverCertificateUntrusted))
        let client = makeClient()
        do {
            _ = try await client.listNotes(limit: 30, cursor: nil)
            XCTFail("expected an error")
        } catch let error as APIClientError {
            XCTAssertEqual(error, .localTrustChanged)
        } catch {
            XCTFail("wrong error type: \(error)")
        }
    }

    func test404SurfacesServerError() async {
        StubURLProtocol.enqueue(status: 404, json: "{\"error\":\"not found\"}")
        let client = makeClient()
        do {
            _ = try await client.noteDetail(id: "missing")
            XCTFail("expected an error")
        } catch let error as APIClientError {
            XCTAssertEqual(error, .server(status: 404, message: "not found"))
        } catch {
            XCTFail("wrong error type: \(error)")
        }
    }

    func testInvalidLoginSurfacesServerMessage() async {
        StubURLProtocol.enqueue(status: 401, json: "{\"error\":\"invalid credentials\"}")
        let client = makeClient(token: nil)
        do {
            _ = try await client.login(email: "user@example.com", password: "wrong")
            XCTFail("expected an error")
        } catch let error as APIClientError {
            // Login itself is unauthenticated (authorized: false), so a 401
            // here is a normal server error, not sessionEnded.
            XCTAssertEqual(error, .server(status: 401, message: "invalid credentials"))
        } catch {
            XCTFail("wrong error type: \(error)")
        }
    }
}

extension URLRequest {
    /// URLSession sometimes exposes a POST body only via httpBodyStream
    /// (not httpBody) once a request reaches a custom URLProtocol,
    /// depending on OS version/session configuration. Fall back to draining
    /// the stream so this assertion is robust either way.
    func httpBodyDataForTest() -> Data? {
        if let httpBody { return httpBody }
        guard let stream = httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        let bufferSize = 4096
        var buffer = [UInt8](repeating: 0, count: bufferSize)
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: bufferSize)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data
    }
}
