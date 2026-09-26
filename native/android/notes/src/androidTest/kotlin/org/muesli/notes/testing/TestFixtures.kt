package org.muesli.notes.testing

import org.muesli.notes.model.NoteListItem
import java.time.Instant

/** A minimal, deterministic [NoteListItem] for androidTest Compose UI tests. */
fun testNoteItem(id: String, createdAt: Instant = Instant.parse("2026-01-01T00:00:00Z")): NoteListItem = NoteListItem(
    id = id,
    title = "Note $id",
    status = "ready",
    pinned = false,
    startedAt = null,
    endedAt = null,
    createdAt = createdAt,
    updatedAt = createdAt,
    snippet = "snippet-$id",
    tags = emptyList(),
)

fun testDetail(id: String): org.muesli.notes.model.NoteDetail = org.muesli.notes.model.NoteDetail(
    id = id,
    title = "Note $id",
    status = "ready",
    pinned = false,
    startedAt = null,
    endedAt = null,
    createdAt = Instant.parse("2026-01-01T00:00:00Z"),
    updatedAt = Instant.parse("2026-01-01T00:00:00Z"),
    tags = emptyList(),
    bodyMarkdown = "body",
    summaries = emptyList(),
)

fun testPage(pagePrefix: String, count: Int, nextCursor: String?): org.muesli.notes.model.NotesPage =
    org.muesli.notes.model.NotesPage(items = (0 until count).map { testNoteItem("$pagePrefix-$it") }, nextCursor = nextCursor)
