package org.muesli.notes.testing

import org.muesli.notes.model.NoteListItem
import org.muesli.notes.model.NotesPage
import java.time.Instant

/** A minimal, deterministic [NoteListItem] for repository/view-model tests. */
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

/** Builds a page of [count] deterministic items named "$pagePrefix-$i", 0-indexed. */
fun testPage(pagePrefix: String, count: Int, nextCursor: String?): NotesPage =
    NotesPage(items = (0 until count).map { testNoteItem("$pagePrefix-$it") }, nextCursor = nextCursor)
