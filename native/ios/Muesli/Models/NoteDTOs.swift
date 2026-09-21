import Foundation

/// DTOs decoded from GET /api/mobile/v1/notes and
/// GET /api/mobile/v1/notes/{id} (issue #767). Field sets intentionally
/// mirror the server's field-minimized contract exactly -- no owner id,
/// folders, event/audio metadata, transcript, or (in the list) snippet-in-
/// detail. Decoding happens off the main actor (see NotesListModel /
/// NoteReaderModel); these types themselves are plain, thread-safe value
/// types.
public struct MobileNoteItem: Decodable, Identifiable, Equatable {
    public let id: String
    public let title: String
    public let status: String
    public let pinned: Bool
    public let startedAt: Date?
    public let endedAt: Date?
    public let createdAt: Date
    public let updatedAt: Date
    public let snippet: String
    public let tags: [String]

    enum CodingKeys: String, CodingKey {
        case id, title, status, pinned, snippet, tags
        case startedAt = "started_at"
        case endedAt = "ended_at"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

public struct MobileNotesListResponse: Decodable {
    public let items: [MobileNoteItem]
    public let nextCursor: String?

    enum CodingKeys: String, CodingKey {
        case items
        case nextCursor = "next_cursor"
    }
}

public struct MobileNoteDetailMeta: Decodable, Equatable {
    public let id: String
    public let title: String
    public let status: String
    public let pinned: Bool
    public let startedAt: Date?
    public let endedAt: Date?
    public let createdAt: Date
    public let updatedAt: Date
    public let tags: [String]

    enum CodingKeys: String, CodingKey {
        case id, title, status, pinned, tags
        case startedAt = "started_at"
        case endedAt = "ended_at"
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }
}

public struct MobileSummarySection: Decodable, Equatable {
    public let heading: String
    public let contentMarkdown: String

    enum CodingKeys: String, CodingKey {
        case heading
        case contentMarkdown = "content_markdown"
    }
}

public struct MobileSummary: Decodable, Identifiable, Equatable {
    public let id: String
    public let templateName: String
    public let status: String
    public let truncated: Bool
    public let sections: [MobileSummarySection]

    enum CodingKeys: String, CodingKey {
        case id, status, truncated, sections
        case templateName = "template_name"
    }
}

public struct MobileNoteDetailResponse: Decodable {
    public let note: MobileNoteDetailMeta
    public let bodyMarkdown: String
    public let summaries: [MobileSummary]

    enum CodingKeys: String, CodingKey {
        case note, summaries
        case bodyMarkdown = "body_markdown"
    }
}

/// A shared RFC3339-decoding JSONDecoder for every mobile API response.
public enum MobileAPIDecoding {
    public static func makeDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        let iso = ISO8601DateFormatter()
        iso.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let isoNoFraction = ISO8601DateFormatter()
        isoNoFraction.formatOptions = [.withInternetDateTime]
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let raw = try container.decode(String.self)
            if let date = iso.date(from: raw) ?? isoNoFraction.date(from: raw) {
                return date
            }
            throw DecodingError.dataCorruptedError(
                in: container, debugDescription: "Invalid RFC3339 date: \(raw)")
        }
        return decoder
    }
}
