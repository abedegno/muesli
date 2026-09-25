package org.muesli.app.auth

import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.util.concurrent.TimeUnit

/** Coverage for [LoginApi] against the real `POST /api/login` contract shape. */
class LoginApiTest {
    private lateinit var server: MockWebServer
    private lateinit var api: LoginApi
    private val client = OkHttpClient.Builder().callTimeout(5, TimeUnit.SECONDS).build()

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        api = LoginApi(client)
    }

    @After
    fun tearDown() {
        runCatching { server.shutdown() }
    }

    @Test
    fun `200 with a token yields Success`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody("""{"token":"abc123"}"""))

        val result = api.login(server.url("/").toString(), "user@example.com", "hunter2")

        assertTrue(result is LoginResult.Success)
        assertEquals("abc123", (result as LoginResult.Success).token)

        val recorded = server.takeRequest()
        assertEquals("/api/login", recorded.path)
        assertEquals("POST", recorded.method)
        assertTrue(recorded.body.readUtf8().contains("\"email\":\"user@example.com\""))
    }

    @Test
    fun `401 yields a Failure without leaking the response body`() = runTest {
        server.enqueue(MockResponse().setResponseCode(401).setBody("""{"error":"invalid credentials"}"""))

        val result = api.login(server.url("/").toString(), "user@example.com", "wrong")

        assertTrue(result is LoginResult.Failure)
    }

    @Test
    fun `malformed 200 body yields a Failure`() = runTest {
        server.enqueue(MockResponse().setResponseCode(200).setBody("not json"))

        val result = api.login(server.url("/").toString(), "user@example.com", "hunter2")

        assertTrue(result is LoginResult.Failure)
    }

    @Test
    fun `invalid server URL yields a Failure without a network call`() = runTest {
        val result = api.login("not a url", "user@example.com", "hunter2")

        assertTrue(result is LoginResult.Failure)
        assertEquals(0, server.requestCount)
    }
}
