package org.muesli.notes.model

import java.time.Instant

/**
 * Immutable, presentation-agnostic domain models mapped from the #767
 * mobile contract DTOs (see org.muesli.notes.api.ApiModels). These carry no
 * fallback/guessed values: every field here is either present in the
 * contract response or explicitly optional (nullable / empty collection) per
 * that contract.
 */
data class NoteListItem(
    val id: String,
    val title: String,
    val status: String,
    val pinned: Boolean,
    val startedAt: Instant?,
    val endedAt: Instant?,
    val createdAt: Instant,
    val updatedAt: Instant,
    val snippet: String,
    val tags: List<String>,
)

/** One bounded page of notes plus the opaque continuation cursor, or null when terminal. */
data class NotesPage(
    val items: List<NoteListItem>,
    val nextCursor: String?,
)

data class NoteSummarySection(
    val heading: String,
    val contentMarkdown: String,
)

data class NoteSummary(
    val id: String,
    val templateName: String,
    val status: String,
    val truncated: Boolean,
    val sections: List<NoteSummarySection>,
)

/** The authoritative, complete note detail -- never a truncated list projection. */
data class NoteDetail(
    val id: String,
    val title: String,
    val status: String,
    val pinned: Boolean,
    val startedAt: Instant?,
    val endedAt: Instant?,
    val createdAt: Instant,
    val updatedAt: Instant,
    val tags: List<String>,
    val bodyMarkdown: String,
    val summaries: List<NoteSummary>,
)
