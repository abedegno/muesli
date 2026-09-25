package org.muesli.notes.model

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import org.muesli.notes.api.ContractViolationException
import org.muesli.notes.api.MobileNoteDetailResponseDto
import org.muesli.notes.api.MobileNoteItemDto
import org.muesli.notes.api.toDomain
import java.time.Instant

/**
 * Pure mapping-layer tests (issue #768 Task 2): DTO -> domain, independent of
 * any network transport. ApiModels' DTOs/toDomain() are `internal`, visible
 * here because Kotlin treats an Android module's unit-test source set as a
 * friend of its main source set.
 */
class NoteMappingTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `list item maps every field including optional timestamps`() {
        val raw = """
            {"id":"n1","title":"T","status":"ready","pinned":true,
             "started_at":"2026-01-10T09:00:00Z","ended_at":"2026-01-10T09:45:00Z",
             "created_at":"2026-01-10T09:00:05Z","updated_at":"2026-01-10T09:45:10Z",
             "snippet":"s","tags":["a","b"]}
        """.trimIndent()
        val item = json.decodeFromString<MobileNoteItemDto>(raw).toDomain()
        assertEquals("n1", item.id)
        assertEquals(Instant.parse("2026-01-10T09:00:00Z"), item.startedAt)
        assertEquals(listOf("a", "b"), item.tags)
    }

    @Test
    fun `list item with absent optional timestamps maps to null`() {
        val raw = """
            {"id":"n1","title":"T","status":"ready","pinned":false,
             "created_at":"2026-01-10T09:00:05Z","updated_at":"2026-01-10T09:45:10Z",
             "snippet":"","tags":[]}
        """.trimIndent()
        val item = json.decodeFromString<MobileNoteItemDto>(raw).toDomain()
        assertNull(item.startedAt)
        assertNull(item.endedAt)
        assertTrue(item.tags.isEmpty())
    }

    @Test
    fun `blank list item id is a contract violation`() {
        val raw = """
            {"id":"","title":"T","status":"ready","pinned":false,
             "created_at":"2026-01-10T09:00:05Z","updated_at":"2026-01-10T09:45:10Z",
             "snippet":"","tags":[]}
        """.trimIndent()
        assertThrows(ContractViolationException::class.java) {
            json.decodeFromString<MobileNoteItemDto>(raw).toDomain()
        }
    }

    @Test
    fun `unparseable timestamp is a contract violation`() {
        val raw = """
            {"id":"n1","title":"T","status":"ready","pinned":false,
             "created_at":"not-a-timestamp","updated_at":"2026-01-10T09:45:10Z",
             "snippet":"","tags":[]}
        """.trimIndent()
        assertThrows(ContractViolationException::class.java) {
            json.decodeFromString<MobileNoteItemDto>(raw).toDomain()
        }
    }

    @Test
    fun `detail maps complete optional fields and multiple summaries in order`() {
        val raw = """
            {"note":{"id":"n1","title":"T","status":"ready","pinned":true,
              "started_at":"2026-01-10T09:00:00Z","ended_at":"2026-01-10T09:45:00Z",
              "created_at":"2026-01-10T09:00:05Z","updated_at":"2026-01-10T09:45:10Z","tags":["x"]},
             "body_markdown":"body",
             "summaries":[
               {"id":"s1","template_name":"A","status":"ready","truncated":true,
                "sections":[{"heading":"H","content_markdown":"C"}]},
               {"id":"s2","template_name":"B","status":"pending","truncated":false,"sections":[]}
             ]}
        """.trimIndent()
        val detail = json.decodeFromString<MobileNoteDetailResponseDto>(raw).toDomain()
        assertEquals(2, detail.summaries.size)
        assertEquals("s1", detail.summaries[0].id)
        assertEquals("H", detail.summaries[0].sections[0].heading)
        assertTrue(detail.summaries[1].sections.isEmpty())
    }

    @Test
    fun `detail with a blank note id is a contract violation`() {
        val raw = """
            {"note":{"id":"","title":"T","status":"ready","pinned":false,
              "created_at":"2026-01-10T09:00:05Z","updated_at":"2026-01-10T09:45:10Z","tags":[]},
             "body_markdown":"","summaries":[]}
        """.trimIndent()
        assertThrows(ContractViolationException::class.java) {
            json.decodeFromString<MobileNoteDetailResponseDto>(raw).toDomain()
        }
    }
}
