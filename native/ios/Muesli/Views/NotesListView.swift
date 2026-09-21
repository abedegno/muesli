import SwiftUI

struct NotesListView: View {
    let configuration: ServerConfiguration
    @EnvironmentObject private var environment: AppEnvironment
    @StateObject private var model: NotesListModel

    /// `model` is constructed by the caller (RootView, via
    /// AppEnvironment.makeNotesListModel()) so it is already wired to the
    /// real, environment-owned APIClient/token provider -- this view never
    /// constructs its own network client.
    init(configuration: ServerConfiguration, model: NotesListModel) {
        self.configuration = configuration
        _model = StateObject(wrappedValue: model)
    }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Notes")
                .toolbar {
                    ToolbarItem(placement: .navigationBarTrailing) {
                        Button("Sign Out") {
                            environment.sessionStore.signOut(configuration: configuration)
                        }
                    }
                }
        }
        .task {
            await model.loadFirstPage()
        }
        .refreshable {
            await model.refresh()
        }
    }

    @ViewBuilder
    private var content: some View {
        switch model.loadState {
        case .idle, .loadingFirstPage:
            ProgressView("Loading notes…")
        case .empty:
            ContentUnavailableCompat(title: "No notes yet", message: "Notes you create will appear here.")
        case .error(let message):
            ContentUnavailableCompat(title: "Couldn't load notes", message: message)
        case .loaded:
            List {
                ForEach(model.items) { item in
                    NavigationLink(value: item.id) {
                        NoteRow(item: item)
                    }
                }
                if model.isLoadingNextPage {
                    ProgressView().frame(maxWidth: .infinity)
                } else if let error = model.nextPageError {
                    Button("Retry: \(error)") { model.retryNextPage() }
                        .foregroundStyle(.red)
                } else {
                    Color.clear.frame(height: 1)
                        .onAppear { model.loadNextPageIfNeeded() }
                }
            }
            .navigationDestination(for: String.self) { noteID in
                NoteReaderView(noteID: noteID)
            }
        }
    }

}

struct NoteRow: View {
    let item: MobileNoteItem

    private var bestDate: Date { item.startedAt ?? item.createdAt }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                if item.pinned {
                    Image(systemName: "pin.fill")
                        .accessibilityLabel("Pinned")
                }
                Text(item.title.isEmpty ? "Untitled" : item.title)
                    .font(.headline)
            }
            HStack(spacing: 6) {
                Text(NoteStatusPresentation.label(for: item.status))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Text(bestDate, style: .date)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            if !item.snippet.isEmpty {
                Text(item.snippet)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            "\(item.title.isEmpty ? "Untitled" : item.title), \(NoteStatusPresentation.label(for: item.status))"
                + (item.pinned ? ", pinned" : ""))
    }
}

/// A minimal stand-in for iOS 17's ContentUnavailableView, kept dependency-
/// free and usable regardless of exact SDK availability nuances.
struct ContentUnavailableCompat: View {
    let title: String
    let message: String

    var body: some View {
        VStack(spacing: 8) {
            Text(title).font(.headline)
            Text(message).font(.subheadline).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .combine)
    }
}
