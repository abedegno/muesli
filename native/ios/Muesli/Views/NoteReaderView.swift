import SwiftUI

struct NoteReaderView: View {
    let noteID: String
    @EnvironmentObject private var environment: AppEnvironment
    @State private var model: NoteReaderModel?

    var body: some View {
        Group {
            if let model {
                NoteReaderContent(model: model)
                    .environmentObject(model)
            } else {
                ProgressView()
            }
        }
        .task {
            guard model == nil else { return }
            let newModel = environment.makeNoteReaderModel(noteID: noteID)
            model = newModel
            await newModel?.load()
        }
    }
}

private struct NoteReaderContent: View {
    @ObservedObject var model: NoteReaderModel

    var body: some View {
        content
            .refreshable { await model.refresh() }
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .loading:
            ProgressView("Loading…")
        case .notFound:
            ContentUnavailableCompat(
                title: "Note unavailable",
                message: "This note may have been deleted or is no longer available to you.")
        case .error(let message):
            ContentUnavailableCompat(title: "Couldn't load this note", message: message)
        case .loaded(let detail):
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    header(detail.note)
                    if let banner = NoteStatusPresentation.readinessBannerText(for: detail.note.status) {
                        StatusBanner(text: banner, isError: false)
                    } else if NoteStatusPresentation.isFailed(detail.note.status) {
                        StatusBanner(text: "Processing failed. Content below may be incomplete.", isError: true)
                    } else if !NoteStatusPresentation.isKnown(detail.note.status) {
                        StatusBanner(text: NoteStatusPresentation.unsupportedStatusText, isError: false)
                    }
                    if !detail.note.tags.isEmpty {
                        TagsRow(tags: detail.note.tags)
                    }
                    SafeMarkdownView(markdown: detail.bodyMarkdown)
                        .textSelection(.enabled)
                    ForEach(detail.summaries) { summary in
                        SummaryCard(summary: summary)
                    }
                }
                .padding()
            }
        }
    }

    private func header(_ note: MobileNoteDetailMeta) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(note.title.isEmpty ? "Untitled" : note.title)
                .font(.title2.bold())
            HStack(spacing: 6) {
                Text(NoteStatusPresentation.label(for: note.status)).font(.caption).foregroundStyle(.secondary)
                Text(note.startedAt ?? note.createdAt, style: .date).font(.caption).foregroundStyle(.secondary)
                if note.pinned {
                    Image(systemName: "pin.fill").accessibilityLabel("Pinned")
                }
            }
        }
    }
}

private struct StatusBanner: View {
    let text: String
    let isError: Bool

    var body: some View {
        Text(text)
            .font(.subheadline)
            .padding(8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(isError ? Color.red.opacity(0.15) : Color.yellow.opacity(0.2))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .accessibilityLabel(text)
    }
}

private struct TagsRow: View {
    let tags: [String]

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack {
                ForEach(tags, id: \.self) { tag in
                    Text(tag)
                        .font(.caption)
                        .padding(.horizontal, 8).padding(.vertical, 4)
                        .background(Color.secondary.opacity(0.15))
                        .clipShape(Capsule())
                }
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Tags: \(tags.joined(separator: ", "))")
    }
}

private struct SummaryCard: View {
    let summary: MobileSummary

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(summary.templateName.isEmpty ? "Summary" : summary.templateName)
                    .font(.headline)
                Spacer()
                Text(NoteStatusPresentation.label(for: summary.status))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            if summary.truncated {
                StatusBanner(text: "This summary may have been cut short.", isError: false)
            }
            if let banner = NoteStatusPresentation.readinessBannerText(for: summary.status) {
                StatusBanner(text: banner, isError: false)
            } else if NoteStatusPresentation.isFailed(summary.status) {
                StatusBanner(text: "This summary failed to generate.", isError: true)
            }
            ForEach(Array(summary.sections.enumerated()), id: \.offset) { _, section in
                VStack(alignment: .leading, spacing: 4) {
                    if !section.heading.isEmpty {
                        Text(section.heading).font(.subheadline.bold())
                    }
                    SafeMarkdownView(markdown: section.contentMarkdown)
                        .textSelection(.enabled)
                }
            }
        }
        .padding()
        .background(Color.secondary.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: 12))
    }
}
