package org.muesli.app.session

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class AppSessionSourceTest {

    @Test
    fun `signed out by default`() {
        val source = AppSessionSource()
        assertNull(source.session.value)
    }

    @Test
    fun `signIn installs a session with headers derived from the token`() {
        val source = AppSessionSource()
        source.signIn(token = "tok-123", baseUrl = "https://muesli.example/")

        val session = source.session.value
        assertNotNull(session)
        assertEquals("https://muesli.example/", session!!.baseUrl)
        assertEquals(mapOf("Authorization" to "Bearer tok-123"), session.headers())
    }

    @Test
    fun `signOut clears the session unconditionally`() {
        val source = AppSessionSource()
        source.signIn("tok", "https://muesli.example/")
        source.signOut()
        assertNull(source.session.value)
    }

    @Test
    fun `invalidate ends the exact matching session and returns true`() {
        val source = AppSessionSource()
        source.signIn("tok", "https://muesli.example/")
        val session = source.session.value!!

        val ended = source.invalidate(session.id, session.generation)

        assertTrue(ended)
        assertNull(source.session.value)
    }

    @Test
    fun `invalidate is a no-op for a superseded session`() {
        val source = AppSessionSource()
        source.signIn("tok-1", "https://muesli.example/")
        val firstSession = source.session.value!!

        // A new sign-in supersedes the first session (e.g. a different
        // account signing in) before the stale invalidate call arrives.
        source.signIn("tok-2", "https://muesli.example/")

        val ended = source.invalidate(firstSession.id, firstSession.generation)

        assertFalse(ended)
        assertNotNull(source.session.value)
        assertEquals(mapOf("Authorization" to "Bearer tok-2"), source.session.value!!.headers())
    }

    @Test
    fun `invalidate on an already signed-out session is a no-op`() {
        val source = AppSessionSource()
        source.signIn("tok", "https://muesli.example/")
        val session = source.session.value!!
        source.signOut()

        val ended = source.invalidate(session.id, session.generation)

        assertFalse(ended)
        assertNull(source.session.value)
    }
}
