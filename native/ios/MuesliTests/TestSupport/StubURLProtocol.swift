import Foundation

/// A minimal, queue-based URLProtocol stub so APIClient/model tests exercise
/// the real APIClient class against canned HTTP responses instead of a
/// hand-duplicated fake client -- higher fidelity for request-shape and
/// decoding assertions (issue #767).
final class StubURLProtocol: URLProtocol {
    struct Stub {
        let status: Int
        let body: Data
        let headers: [String: String]

        init(status: Int, json: String, headers: [String: String] = ["Content-Type": "application/json"]) {
            self.status = status
            self.body = Data(json.utf8)
            self.headers = headers
        }
    }

    /// Either a canned HTTP response or a transport-level failure -- the
    /// latter is what lets a test drive APIClient.performRequest's
    /// URLError-to-APIClientError.localTrustChanged mapping through a real
    /// request, the same way a Stub drives an HTTP-status response.
    enum Outcome {
        case response(Stub)
        case failure(Error)
    }

    /// FIFO queue of (matcher, outcome) pairs consumed in order per request.
    static var queue: [(matches: (URLRequest) -> Bool, outcome: Outcome)] = []
    static var recordedRequests: [URLRequest] = []

    static func reset() {
        queue = []
        recordedRequests = []
    }

    static func enqueue(status: Int, json: String, matches: @escaping (URLRequest) -> Bool = { _ in true }) {
        queue.append((matches, .response(Stub(status: status, json: json))))
    }

    /// Enqueues a transport-level failure (e.g. `URLError(.serverCertificateUntrusted)`)
    /// for the next matching request, instead of a canned HTTP response.
    static func enqueueFailure(_ error: Error, matches: @escaping (URLRequest) -> Bool = { _ in true }) {
        queue.append((matches, .failure(error)))
    }

    static func makeSession() -> URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [StubURLProtocol.self]
        return URLSession(configuration: config)
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        Self.recordedRequests.append(request)
        guard let index = Self.queue.firstIndex(where: { $0.matches(request) }) else {
            client?.urlProtocol(self, didFailWithError: URLError(.unsupportedURL))
            return
        }
        let outcome = Self.queue[index].outcome
        Self.queue.remove(at: index)
        switch outcome {
        case .response(let stub):
            let response = HTTPURLResponse(
                url: request.url!, statusCode: stub.status, httpVersion: "HTTP/1.1", headerFields: stub.headers)!
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: stub.body)
            client?.urlProtocolDidFinishLoading(self)
        case .failure(let error):
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}
