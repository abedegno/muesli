import Foundation

/// Drives the notes list screen (issue #767): paginated fetch, refresh
/// (replaces state), near-end loading (fetch next page once, coalesced),
/// ID-based merge, retained rows + inline retry on a later-page failure, and
/// credential clearing on an authenticated 401. Decoding happens inside
/// APIClient off the main actor; this type publishes on the main actor.
@MainActor
public final class NotesListModel: ObservableObject {
    public enum LoadState: Equatable {
        case idle
        case loadingFirstPage
        case loaded
        case empty
        case error(String)
    }

    @Published public private(set) var items: [MobileNoteItem] = []
    @Published public private(set) var loadState: LoadState = .idle
    @Published public private(set) var isLoadingNextPage = false
    @Published public private(set) var nextPageError: String?

    private let client: APIClient
    private let pageSize: Int
    private var nextCursor: String?
    private var hasMore = true
    private var inFlightNextPage: Task<Void, Never>?
    /// Set by the caller (SessionStore) when a request throws
    /// APIClientError.sessionEnded, so this model doesn't own session state.
    public var onSessionEnded: (() -> Void)?
    /// Set by the caller when a request throws APIClientError.localTrustChanged.
    public var onLocalTrustChanged: (() -> Void)?

    public init(client: APIClient, pageSize: Int = 30) {
        self.client = client
        self.pageSize = pageSize
    }

    public func loadFirstPage() async {
        loadState = .loadingFirstPage
        do {
            let page = try await client.listNotes(limit: pageSize, cursor: nil)
            items = page.items
            nextCursor = page.nextCursor
            hasMore = page.nextCursor != nil
            loadState = page.items.isEmpty ? .empty : .loaded
        } catch {
            handleFirstPageError(error)
        }
    }

    /// Pull-to-refresh: replaces list state entirely (a fresh traversal).
    public func refresh() async {
        inFlightNextPage?.cancel()
        inFlightNextPage = nil
        nextCursor = nil
        hasMore = true
        nextPageError = nil
        await loadFirstPage()
    }

    /// Called when the list scrolls near the end. Coalesces concurrent
    /// calls onto one in-flight fetch and is a no-op once the final page has
    /// been reached.
    public func loadNextPageIfNeeded() {
        guard hasMore, inFlightNextPage == nil, let cursor = nextCursor else { return }
        isLoadingNextPage = true
        nextPageError = nil
        inFlightNextPage = Task { [weak self] in
            guard let self else { return }
            do {
                let page = try await self.client.listNotes(limit: self.pageSize, cursor: cursor)
                await self.mergeNextPage(page)
            } catch {
                await self.handleNextPageError(error)
            }
        }
    }

    public func retryNextPage() {
        inFlightNextPage = nil
        loadNextPageIfNeeded()
    }

    private func mergeNextPage(_ page: MobileNotesListResponse) {
        var seen = Set(items.map(\.id))
        for item in page.items where !seen.contains(item.id) {
            items.append(item)
            seen.insert(item.id)
        }
        nextCursor = page.nextCursor
        hasMore = page.nextCursor != nil
        isLoadingNextPage = false
        inFlightNextPage = nil
    }

    private func handleFirstPageError(_ error: Error) {
        if let apiError = error as? APIClientError {
            switch apiError {
            case .sessionEnded:
                onSessionEnded?()
                return
            case .localTrustChanged:
                items = []
                onLocalTrustChanged?()
                return
            default:
                break
            }
        }
        loadState = .error(Self.message(for: error))
    }

    private func handleNextPageError(_ error: Error) {
        isLoadingNextPage = false
        if let apiError = error as? APIClientError {
            switch apiError {
            case .sessionEnded:
                onSessionEnded?()
                return
            case .localTrustChanged:
                items = []
                onLocalTrustChanged?()
                return
            default:
                break
            }
        }
        // Later-page failure preserves rows and offers inline retry.
        nextPageError = Self.message(for: error)
    }

    private static func message(for error: Error) -> String {
        if let apiError = error as? APIClientError {
            switch apiError {
            case .server(_, let message): return message
            case .transport(let message): return message
            case .decoding: return "Unexpected response from server."
            case .notAuthenticated: return "Not signed in."
            case .sessionEnded: return "Your session ended."
            case .localTrustChanged: return "The local connection changed."
            }
        }
        return "Something went wrong."
    }
}
