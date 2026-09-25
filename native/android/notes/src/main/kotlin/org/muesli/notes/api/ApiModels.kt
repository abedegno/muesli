package org.muesli.notes.api

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import org.muesli.notes.model.NoteDetail
import org.muesli.notes.model.NoteListItem
import org.muesli.notes.model.NoteSummary
import org.muesli.notes.model.NoteSummarySection
import org.muesli.notes.model.NotesPage
import java.time.Instant
import java.time.format.DateTimeParseException

/**
 * Thrown when a 2xx body decodes structurally but violates a contract
 * invariant this client depends on (a blank id, an unparseable timestamp).
 * [OkHttpMobileNotesApi] catches this alongside decode failures and maps
 * both to [ApiFailure.MalformedPayload] -- never surfaced to callers.
 */
internal class ContractViolationException(message: String) : Exception(message)

// The DTOs below mirror internal/api/mobile_notes.go's wire shapes exactly
// (field names, optionality, JSON keys) -- see
// native/android/notes/src/test/resources/contract/PROVENANCE.md. They are
// never used directly outside this file; toDomain() below is the only
// bridge to the presentation-agnostic model package.

@Serializable
internal data class MobileNoteItemDto(
    val id: String,
    val title: String,
    val status: String,
    val pinned: Boolean,
    @SerialName("started_at") val startedAt: String? = null,
    @SerialName("ended_at") val endedAt: String? = null,
    @SerialName("created_at") val createdAt: String,
    @SerialName("updated_at") val updatedAt: String,
    val snippet: String,
    val tags: List<String>,
)

@Serializable
internal data class MobileNotesListResponseDto(
    val items: List<MobileNoteItemDto>,
    @SerialName("next_cursor") val nextCursor: String? = null,
)

@Serializable
internal data class MobileSummarySectionDto(
    val heading: String,
    @SerialName("content_markdown") val contentMarkdown: String,
)

@Serializable
internal data class MobileSummaryDto(
    val id: String,
    @SerialName("template_name") val templateName: String,
    val status: String,
    val truncated: Boolean,
    val sections: List<MobileSummarySectionDto>,
)

@Serializable
internal data class MobileNoteDetailNoteDto(
    val id: String,
    val title: String,
    val status: String,
    val pinned: Boolean,
    @SerialName("started_at") val startedAt: String? = null,
    @SerialName("ended_at") val endedAt: String? = null,
    @SerialName("created_at") val createdAt: String,
    @SerialName("updated_at") val updatedAt: String,
    val tags: List<String>,
)

@Serializable
internal data class MobileNoteDetailResponseDto(
    val note: MobileNoteDetailNoteDto,
    @SerialName("body_markdown") val bodyMarkdown: String,
    val summaries: List<MobileSummaryDto>,
)

@Serializable
internal data class ErrorBodyDto(val error: String)

private fun requiredInstant(raw: String, field: String): Instant = try {
    Instant.parse(raw)
} catch (e: DateTimeParseException) {
    throw ContractViolationException("invalid $field: $raw")
}

private fun optionalInstant(raw: String?, field: String): Instant? = raw?.let { requiredInstant(it, field) }

internal fun MobileNoteItemDto.toDomain(): NoteListItem {
    if (id.isBlank()) throw ContractViolationException("list item missing id")
    return NoteListItem(
        id = id,
        title = title,
        status = status,
        pinned = pinned,
        startedAt = optionalInstant(startedAt, "started_at"),
        endedAt = optionalInstant(endedAt, "ended_at"),
        createdAt = requiredInstant(createdAt, "created_at"),
        updatedAt = requiredInstant(updatedAt, "updated_at"),
        snippet = snippet,
        tags = tags,
    )
}

internal fun MobileNotesListResponseDto.toDomain(): NotesPage =
    NotesPage(items = items.map { it.toDomain() }, nextCursor = nextCursor?.takeIf { it.isNotBlank() })

internal fun MobileSummarySectionDto.toDomain(): NoteSummarySection = NoteSummarySection(heading, contentMarkdown)

internal fun MobileSummaryDto.toDomain(): NoteSummary {
    if (id.isBlank()) throw ContractViolationException("summary missing id")
    return NoteSummary(
        id = id,
        templateName = templateName,
        status = status,
        truncated = truncated,
        sections = sections.map { it.toDomain() },
    )
}

internal fun MobileNoteDetailResponseDto.toDomain(): NoteDetail {
    if (note.id.isBlank()) throw ContractViolationException("detail missing note id")
    return NoteDetail(
        id = note.id,
        title = note.title,
        status = note.status,
        pinned = note.pinned,
        startedAt = optionalInstant(note.startedAt, "started_at"),
        endedAt = optionalInstant(note.endedAt, "ended_at"),
        createdAt = requiredInstant(note.createdAt, "created_at"),
        updatedAt = requiredInstant(note.updatedAt, "updated_at"),
        tags = note.tags,
        bodyMarkdown = bodyMarkdown,
        summaries = summaries.map { it.toDomain() },
    )
}
