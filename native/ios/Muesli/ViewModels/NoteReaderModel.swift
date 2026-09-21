import Foundation

/// Drives the note reader (issue #767): fetches current detail on
/// selection, refreshes (no polling) on pull-to-refresh, preserves partial
/// content across a non-auth failure, and reports 401/404 for the caller to
/// route (session-ended vs. "note unavailable, back to a refreshed list").
@MainActor
public final class NoteReaderModel: ObservableObject {
    public enum LoadState: Equatable {
        case loading
        case loaded(MobileNoteDetailResponse)
        case notFound
        case error(String)

        public static func == (lhs: LoadState, rhs: LoadState) -> Bool {
            switch (lhs, rhs) {
            case (.loading, .loading): return true
            case (.notFound, .notFound): return true
            case let (.loaded(a), .loaded(b)): return a.note == b.note && a.bodyMarkdown == b.bodyMarkdown
            case let (.error(a), .error(b)): return a == b
            default: return false
            }
        }
    }

    @Published public private(set) var state: LoadState = .loading
    private let client: APIClient
    private let noteID: String
    private var currentTask: Task<Void, Never>?
    public var onSessionEnded: (() -> Void)?
    public var onLocalTrustChanged: (() -> Void)?

    public init(client: APIClient, noteID: String) {
        self.client = client
        self.noteID = noteID
    }

    public func load() async {
        currentTask?.cancel()
        let task = Task { await self.fetch() }
        currentTask = task
        await task.value
    }

    /// Pull-to-refresh reloads detail; the reader never polls on its own.
    public func refresh() async {
        await fetch()
    }

    private func fetch() async {
        let previous = state
        state = .loading
        do {
            let detail = try await client.noteDetail(id: noteID)
            state = .loaded(detail)
        } catch let apiError as APIClientError {
            switch apiError {
            case .sessionEnded:
                onSessionEnded?()
            case .server(let status, _) where status == 404:
                state = .notFound
            case .localTrustChanged:
                onLocalTrustChanged?()
            default:
                // Transport/decode/non-auth HTTP errors preserve already
                // loaded content and offer retry.
                if case .loaded = previous {
                    state = previous
                } else {
                    state = .error(Self.message(for: apiError))
                }
            }
        } catch {
            if case .loaded = previous {
                state = previous
            } else {
                state = .error("Something went wrong.")
            }
        }
    }

    private static func message(for error: APIClientError) -> String {
        switch error {
        case .server(_, let message): return message
        case .transport(let message): return message
        case .decoding: return "Unexpected response from server."
        case .notAuthenticated: return "Not signed in."
        case .sessionEnded: return "Your session ended."
        case .localTrustChanged: return "The local connection changed."
        }
    }
}
