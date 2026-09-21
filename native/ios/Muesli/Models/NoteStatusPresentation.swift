import Foundation

/// One shared status-label table for the list and reader (issue #767): both
/// screens must present the exact same label for the exact same status, and
/// both must decode an unrecognized status safely rather than crashing.
public enum NoteStatusPresentation {
    /// draft, recording, uploaded, transcribing, summarizing are the five
    /// nonterminal statuses; `ready` and `failed` are terminal.
    public static let nonterminalStatuses: Set<String> = [
        "draft", "recording", "uploaded", "transcribing", "summarizing",
    ]

    public static func label(for status: String) -> String {
        switch status {
        case "draft": return "Draft"
        case "recording": return "Recording"
        case "uploaded": return "Uploaded"
        case "transcribing": return "Transcribing"
        case "summarizing": return "Summarizing"
        case "ready": return "Ready"
        case "failed": return "Failed"
        default: return "Unknown"
        }
    }

    public static func isNonterminal(_ status: String) -> Bool {
        nonterminalStatuses.contains(status)
    }

    public static func isReady(_ status: String) -> Bool { status == "ready" }
    public static func isFailed(_ status: String) -> Bool { status == "failed" }
    public static func isKnown(_ status: String) -> Bool {
        isReady(status) || isFailed(status) || isNonterminal(status)
    }

    /// The reader's readiness banner text for a nonterminal status, per the
    /// accepted spec ("content is not ready yet — <shared label>").
    public static func readinessBannerText(for status: String) -> String? {
        guard isNonterminal(status) else { return nil }
        return "Content is not ready yet — \(label(for: status))."
    }

    /// Text shown when a status decodes but isn't one this app version
    /// recognizes -- never crashes, never shows a blank status.
    public static let unsupportedStatusText = "Status not supported by this app version."
}
