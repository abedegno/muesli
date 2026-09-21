import SwiftUI

/// Renders authored/summary Markdown as native, selectable text (issue
/// #767). Deliberately NOT a WebView: no HTML/JavaScript execution, no
/// remote image loading, and no non-HTTP(S) URL scheme is ever followed.
/// Unsupported constructs fall back to literal, still-readable text rather
/// than being dropped or crashing the parser.
///
/// Block splitting is manual (split on blank lines, parse each paragraph as
/// inline Markdown) rather than relying on AttributedString's block-level
/// Markdown interpretation, so a parse failure in one paragraph degrades to
/// literal text for just that paragraph instead of the whole note.
struct SafeMarkdownView: View {
    let markdown: String

    private var paragraphs: [String] {
        markdown
            .components(separatedBy: "\n\n")
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if paragraphs.isEmpty {
                EmptyView()
            } else {
                ForEach(Array(paragraphs.enumerated()), id: \.offset) { _, paragraph in
                    Text(Self.safeAttributedString(for: paragraph))
                }
            }
        }
    }

    /// Parses one paragraph as inline Markdown (bold/italic/code/links),
    /// then strips any link whose scheme is not http/https -- a custom,
    /// file, data, or javascript scheme link becomes plain non-tappable
    /// text rather than being followed. Falls back to the literal source
    /// text (still fully readable) if parsing fails entirely.
    static func safeAttributedString(for paragraph: String) -> AttributedString {
        var options = AttributedString.MarkdownParsingOptions()
        options.interpretedSyntax = .inlineOnlyPreservingWhitespace
        guard var attributed = try? AttributedString(markdown: paragraph, options: options) else {
            return AttributedString(paragraph)
        }
        for run in attributed.runs {
            if let link = run.link, !isHTTPOrHTTPS(link) {
                attributed[run.range].link = nil
            }
        }
        return attributed
    }

    private static func isHTTPOrHTTPS(_ url: URL) -> Bool {
        guard let scheme = url.scheme?.lowercased() else { return false }
        return scheme == "http" || scheme == "https"
    }
}
