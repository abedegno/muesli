package org.muesli.notes.api

import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.util.concurrent.TimeUnit

private class FixedSession(
    override val baseUrl: String,
    private val tokenSupplier: () -> String,
) : AuthenticatedSession {
    override val id = SessionId("test-session")
    override val generation = 1L
    override fun headers(): Map<String, String> = mapOf("Authorization" to "Bearer ${tokenSupplier()}")
}

/**
 * MockWebServer coverage for [OkHttpMobileNotesApi] (issue #768 Task 2)
 * against every real fixture in the #767 contract corpus, plus the
 * corruption/cancellation/network cases the plan calls for.
 */
class OkHttpMobileNotesApiTest {
    private lateinit var server: MockWebServer
    private lateinit var api: OkHttpMobileNotesApi
    private val client = OkHttpClient.Builder().callTimeout(5, TimeUnit.SECONDS).build()

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        api = OkHttpMobileNotesApi(client)
    }

    @After
    fun tearDown() {
        runCatching { server.shutdown() }
    }

    private fun fixture(name: String): String =
        javaClass.classLoader!!.getResourceAsStream("contract/$name")!!.readBytes().decodeToString()

    private fun session(token: String = "abc123") =
        FixedSession(server.url("/").toString().trimEnd('/')) { token }

    @Test
    fun `first page request has no cursor query param`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody(fixture("list_empty.json")))
        api.listNotes(session(), limit = 30, cursor = null)
        val recorded = server.takeRequest()
        assertEquals("/api/mobile/v1/notes?limit=30", recorded.path)
    }

    @Test
    fun `cursor page request percent-encodes the opaque cursor`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody(fixture("list_populated_nonterminal.json")))
        val weirdCursor = "abc+def/ghi=="
        api.listNotes(session(), limit = 30, cursor = weirdCursor)
        val recorded = server.takeRequest()
        assertTrue(recorded.path!!.contains("cursor="))
        assertFalse("raw '+' must be percent-encoded, not left to be decoded as a space", recorded.path!!.contains("cursor=abc+def"))
    }

    @Test
    fun `note id is percent-encoded in the path`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody(fixture("detail_complete.json")))
        api.getNote(session(), noteId = "weird id/with slash")
        val recorded = server.takeRequest()
        assertFalse(recorded.path!!.contains(" "))
        assertFalse("a '/' inside the id must be encoded, not treated as a path separator", recorded.path!!.endsWith("with slash"))
    }

    @Test
    fun `sends a fresh authorization header from the session on every call`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody(fixture("list_empty.json")))
        server.enqueue(MockResponse().setResponseCode(200).setBody(fixture("list_empty.json")))
        var counter = 0
        val rotating = FixedSession(server.url("/").toString().trimEnd('/')) { "token-${++counter}" }
        api.listNotes(rotating, limit = 30, cursor = null)
        api.listNotes(rotating, limit = 30, cursor = null)
        assertEquals("Bearer token-1", server.takeRequest().getHeader("Authorization"))
        assertEquals("Bearer token-2", server.takeRequest().getHeader("Authorization"))
    }

    @Test
    fun `secrets never appear in the request URL`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody(fixture("list_empty.json")))
        api.listNotes(session(token = "super-secret-token"), limit = 30, cursor = null)
        assertFalse(server.takeRequest().path!!.contains("super-secret-token"))
    }

    @Test
    fun `nonterminal populated page maps every field including optional ones`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody(fixture("list_populated_nonterminal.json")))
        val result = api.listNotes(session(), limit = 30, cursor = null) as ApiResult.Success
        assertEquals(2, result.value.items.size)
        assertNotNull(result.value.nextCursor)
        assertNotNull(result.value.items[0].startedAt)
        assertTrue(result.value.items[1].tags.isEmpty())
    }

    @Test
    fun `terminal page has a null next cursor`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody(fixture("list_populated_terminal.json")))
        val result = api.listNotes(session(), limit = 30, cursor = null) as ApiResult.Success
        assertNull(result.value.nextCursor)
    }

    @Test
    fun `empty page decodes to an empty non-null list`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody(fixture("list_empty.json")))
        val result = api.listNotes(session(), limit = 30, cursor = null) as ApiResult.Success
        assertTrue(result.value.items.isEmpty())
    }

    @Test
    fun `complete detail maps optional fields and multiple summaries`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody(fixture("detail_complete.json")))
        val result = api.getNote(session(), "id") as ApiResult.Success
        assertEquals(2, result.value.summaries.size)
        assertNotNull(result.value.startedAt)
        assertTrue(result.value.summaries.any { it.sections.isNotEmpty() })
    }

    @Test
    fun `detail with optional fields absent maps to nulls and empty collections`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody(fixture("detail_optional_absent.json")))
        val result = api.getNote(session(), "id") as ApiResult.Success
        assertNull(result.value.startedAt)
        assertTrue(result.value.tags.isEmpty())
        assertTrue(result.value.summaries.isEmpty())
    }

    @Test
    fun `401 maps to SessionEnded`() = runTest {
        server.enqueue(MockResponse().setResponseCode(401).setBody(fixture("error_401.json")))
        val result = api.listNotes(session(), limit = 30, cursor = null) as ApiResult.Failure
        assertEquals(ApiFailure.SessionEnded, result.reason)
    }

    @Test
    fun `detail 404 maps to Unavailable`() = runTest {
        server.enqueue(MockResponse().setResponseCode(404).setBody(fixture("error_404.json")))
        val result = api.getNote(session(), "missing") as ApiResult.Failure
        assertEquals(ApiFailure.Unavailable, result.reason)
    }

    @Test
    fun `list 500 maps to Http(500)`() = runTest {
        server.enqueue(MockResponse().setResponseCode(500).setBody(fixture("error_500.json")))
        val result = api.listNotes(session(), limit = 30, cursor = null) as ApiResult.Failure
        assertEquals(ApiFailure.Http(500), result.reason)
    }

    @Test
    fun `503 maps to Http(503)`() = runTest {
        server.enqueue(MockResponse().setResponseCode(503).setBody(fixture("error_503.json")))
        val result = api.listNotes(session(), limit = 30, cursor = null) as ApiResult.Failure
        assertEquals(ApiFailure.Http(503), result.reason)
    }

    @Test
    fun `malformed 200 with a missing list item id is MalformedPayload`() = runTest {
        val corrupted = fixture("list_populated_nonterminal.json")
            .replaceFirst("\"id\":\"3f6a5b8e-7c1a-4a9e-9a4e-111111111111\",", "")
        server.enqueue(MockResponse().setResponseCode(200).setBody(corrupted))
        val result = api.listNotes(session(), limit = 30, cursor = null) as ApiResult.Failure
        assertEquals(ApiFailure.MalformedPayload, result.reason)
    }

    @Test
    fun `malformed 200 with an object detail id is MalformedPayload`() = runTest {
        val corrupted = fixture("detail_complete.json")
            .replaceFirst("\"id\":\"3f6a5b8e-7c1a-4a9e-9a4e-111111111111\"", "\"id\":{}")
        server.enqueue(MockResponse().setResponseCode(200).setBody(corrupted))
        val result = api.getNote(session(), "id") as ApiResult.Failure
        assertEquals(ApiFailure.MalformedPayload, result.reason)
    }

    @Test
    fun `network failure maps to Network`() = runTest {
        val deadUrl = server.url("/").toString()
        server.shutdown()
        val result = api.listNotes(FixedSession(deadUrl.trimEnd('/')) { "t" }, limit = 30, cursor = null) as ApiResult.Failure
        assertTrue(result.reason is ApiFailure.Network)
    }

    @Test
    fun `cancellation propagates instead of becoming a failure`() = runTest {
        server.enqueue(
            MockResponse().setResponseCode(200).setBody(fixture("list_empty.json")).setBodyDelay(5, TimeUnit.SECONDS),
        )
        val job = launch { api.listNotes(session(), limit = 30, cursor = null) }
        job.cancelAndJoin()
        assertTrue(job.isCancelled)
    }
}
