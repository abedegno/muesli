package org.muesli.notes

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.security.MessageDigest
import java.time.Instant

/**
 * Verifies the checked-in #767 contract corpus (issue #768 Task 1) byte-for-
 * byte (via SHA-256) and structurally: exact keys/omissions, non-null arrays,
 * RFC3339 timestamps, opaque cursor terminality, and tags-present vs
 * `tags: []`. This is the first failing test the plan calls for -- it fails
 * until the fixtures exist and match these invariants.
 */
class ContractFixtureTest {

    private fun resourceBytes(name: String): ByteArray =
        requireNotNull(javaClass.classLoader?.getResourceAsStream("contract/$name")) {
            "missing fixture contract/$name"
        }.readBytes()

    private fun resourceJson(name: String): JsonElement =
        Json.parseToJsonElement(String(resourceBytes(name), Charsets.UTF_8))

    private fun sha256Hex(bytes: ByteArray): String =
        MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }

    private fun assertRfc3339(value: String) {
        // Throws DateTimeParseException (failing the test) if not RFC3339.
        Instant.parse(value)
    }

    private val expectedSha256 = mapOf(
        "list_populated_nonterminal.json" to "c8e9130790b2a738659481064e846168f5b09805ec143b7553ab8c33f1bc91a2",
        "list_populated_terminal.json" to "365c74a5623393f5368d2d18ed8ad220954fc6cb9e57b6b39c47af20db30528c",
        "list_empty.json" to "eef46741adfc3a9f76294d3b78f37a45f113092ac9d44ee77c7a038a88ff09a1",
        "detail_complete.json" to "f498ee7123b1929c53fca6f1a9d0ef3e1cb189d368fa5b8dfe6c5866b73a65c6",
        "detail_optional_absent.json" to "23ecaece81d5bf26e20e9608079b79acd6b7d1c2054e12da3ad282d9e1b84e0b",
        "error_401.json" to "3341bff062d83e3afeb943f83bbb079f5b45cc050e796ec282dde50b5906cb31",
        "error_404.json" to "b52893b7fcb4d0203d423e9572b847dbb93a1b9a58f76f2c4075b99d9661a50f",
        "error_500.json" to "5b078e4cd38acbba010a54cbb2d420a7c38c9c9ee61d81b79a383cd282ad1db3",
        "error_503.json" to "d89536ac6d0ba50394a0f28ed5fef6990972dfa5d088525bcc144cea8ea168d1",
    )

    @Test
    fun `every fixture matches its recorded SHA-256`() {
        expectedSha256.forEach { (name, expected) ->
            assertEquals("fixture $name changed unexpectedly", expected, sha256Hex(resourceBytes(name)))
        }
    }

    @Test
    fun `empty list has non-null empty items and no next_cursor`() {
        val list = resourceJson("list_empty.json").jsonObject
        assertTrue(list["items"]!!.jsonArray.isEmpty())
        assertFalse(list.containsKey("next_cursor"))
    }

    @Test
    fun `populated nonterminal list has opaque next_cursor and both tag shapes`() {
        val list = resourceJson("list_populated_nonterminal.json").jsonObject
        val items = list["items"]!!.jsonArray
        assertEquals(2, items.size)

        val tagged = items[0].jsonObject
        assertEquals(listOf("planning", "roadmap"), tagged["tags"]!!.jsonArray.map { it.jsonPrimitive.content })
        assertRfc3339(tagged["started_at"]!!.jsonPrimitive.content)
        assertRfc3339(tagged["ended_at"]!!.jsonPrimitive.content)
        assertRfc3339(tagged["created_at"]!!.jsonPrimitive.content)
        assertRfc3339(tagged["updated_at"]!!.jsonPrimitive.content)

        val untagged = items[1].jsonObject
        assertTrue(untagged["tags"]!!.jsonArray.isEmpty())
        assertFalse(untagged.containsKey("started_at"))
        assertFalse(untagged.containsKey("ended_at"))

        val cursor = list["next_cursor"]!!.jsonPrimitive.content
        assertTrue("cursor must be non-empty and opaque (base64url, no padding)", cursor.isNotBlank())
        assertFalse("cursor must not be plain JSON", cursor.trim().startsWith("{"))
    }

    @Test
    fun `populated terminal list has no next_cursor`() {
        val list = resourceJson("list_populated_terminal.json").jsonObject
        assertEquals(1, list["items"]!!.jsonArray.size)
        assertFalse("a terminal page must omit next_cursor", list.containsKey("next_cursor"))
    }

    @Test
    fun `list item field minimization`() {
        val item = resourceJson("list_populated_terminal.json").jsonObject["items"]!!.jsonArray[0].jsonObject
        val expectedKeys = setOf("id", "title", "status", "pinned", "created_at", "updated_at", "snippet", "tags")
        assertEquals(expectedKeys, item.keys)
    }

    @Test
    fun `complete detail has tags, optional timestamps, and a ready summary section`() {
        val detail = resourceJson("detail_complete.json").jsonObject
        assertEquals(setOf("note", "body_markdown", "summaries"), detail.keys)

        val note = detail["note"]!!.jsonObject
        assertEquals(
            setOf("id", "title", "status", "pinned", "started_at", "ended_at", "created_at", "updated_at", "tags"),
            note.keys,
        )
        assertTrue(note["tags"]!!.jsonArray.isNotEmpty())
        assertRfc3339(note["started_at"]!!.jsonPrimitive.content)
        assertRfc3339(note["ended_at"]!!.jsonPrimitive.content)
        assertTrue((detail["body_markdown"]!!.jsonPrimitive.content).isNotEmpty())

        val summaries = detail["summaries"]!!.jsonArray
        assertTrue("expected multiple summaries", summaries.size >= 2)
        val ready = summaries.map { it.jsonObject }.first { it["status"]!!.jsonPrimitive.content == "ready" }
        assertEquals(setOf("id", "template_name", "status", "truncated", "sections"), ready.keys)
        assertTrue(ready["sections"]!!.jsonArray.isNotEmpty())
        val section = ready["sections"]!!.jsonArray[0].jsonObject
        assertEquals(setOf("heading", "content_markdown"), section.keys)

        val pending = summaries.map { it.jsonObject }.first { it["status"]!!.jsonPrimitive.content == "pending" }
        assertTrue(pending["sections"]!!.jsonArray.isEmpty())
    }

    @Test
    fun `detail with optional fields absent omits them and keeps arrays non-null`() {
        val detail = resourceJson("detail_optional_absent.json").jsonObject
        val note = detail["note"]!!.jsonObject
        assertFalse(note.containsKey("started_at"))
        assertFalse(note.containsKey("ended_at"))
        assertTrue(note["tags"]!!.jsonArray.isEmpty())
        assertTrue(detail["summaries"]!!.jsonArray.isEmpty())
        assertEquals("", detail["body_markdown"]!!.jsonPrimitive.content)
    }

    @Test
    fun `error envelopes carry exactly one error field and the expected message`() {
        val expected = mapOf(
            "error_401.json" to "unauthorized",
            "error_404.json" to "not found",
            "error_500.json" to "internal error",
            "error_503.json" to "service unavailable",
        )
        expected.forEach { (name, message) ->
            val body = resourceJson(name).jsonObject
            assertEquals(setOf("error"), body.keys)
            assertEquals(message, body["error"]!!.jsonPrimitive.content)
        }
    }
}
