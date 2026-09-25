package org.muesli.app.session

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import org.muesli.notes.api.AuthenticatedSession
import org.muesli.notes.api.SessionId
import org.muesli.notes.session.SessionSource
import java.util.concurrent.atomic.AtomicLong

/**
 * The host application's [SessionSource] implementation (issue #768 Task
 * 6): a single in-memory authenticated session, established by [signIn]
 * after a successful `POST /api/login` and cleared by [signOut] (an
 * explicit host-initiated sign-out) or by [invalidate] (a 401 the `:notes`
 * feature observed on this exact session). Holds only the bearer token in
 * memory -- nothing is persisted to disk, so this minimal host app always
 * returns to the login screen after a process death. A production app
 * would add a credential store analogous to the iOS client's
 * `CredentialStore`; deliberately out of scope for this repair round.
 */
class AppSessionSource : SessionSource {
    private val _session = MutableStateFlow<AuthenticatedSession?>(null)
    override val session: StateFlow<AuthenticatedSession?> = _session.asStateFlow()

    private val nextId = AtomicLong(0)

    /**
     * Installs a brand new authenticated session for [token] against
     * [baseUrl]. Always mints a fresh [SessionId] (monotonic counter), so a
     * new sign-in can never collide with -- or be mistaken by
     * [invalidate]'s compare-and-set for -- any prior session's identity.
     */
    fun signIn(token: String, baseUrl: String) {
        val id = SessionId("session-${nextId.incrementAndGet()}")
        _session.value = BearerSession(id = id, generation = 0, baseUrl = baseUrl, token = token)
    }

    /** Unconditional host-initiated sign-out (e.g. a "log out" action). */
    fun signOut() {
        _session.value = null
    }

    override fun invalidate(expectedSessionId: SessionId, expectedGeneration: Long): Boolean {
        val current = _session.value ?: return false
        if (current.id != expectedSessionId || current.generation != expectedGeneration) return false
        // A true atomic compare-and-set against the exact object read above:
        // if session has since been replaced (sign-out, a new sign-in, or a
        // concurrent invalidate winning the race) this fails and returns
        // false, per SessionSource.invalidate's documented contract.
        return _session.compareAndSet(current, null)
    }

    private data class BearerSession(
        override val id: SessionId,
        override val generation: Long,
        override val baseUrl: String,
        val token: String,
    ) : AuthenticatedSession {
        override fun headers(): Map<String, String> = mapOf("Authorization" to "Bearer $token")
    }
}
