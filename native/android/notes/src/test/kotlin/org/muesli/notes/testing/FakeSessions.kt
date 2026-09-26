package org.muesli.notes.testing

import kotlinx.coroutines.flow.MutableStateFlow
import org.muesli.notes.api.AuthenticatedSession
import org.muesli.notes.api.SessionId
import org.muesli.notes.session.SessionSource

class FakeAuthenticatedSession(
    private val idValue: String,
    override val baseUrl: String = "https://example.invalid",
    override val generation: Long = 0,
) : AuthenticatedSession {
    override val id: SessionId = SessionId(idValue)
    override fun headers(): Map<String, String> = mapOf("Authorization" to "Bearer $idValue")
}

class FakeSessionSource(initial: AuthenticatedSession? = null) : SessionSource {
    private val _session = MutableStateFlow(initial)
    override val session = _session

    val invalidated = mutableListOf<Pair<SessionId, Long>>()

    // A genuine atomic compare-and-set, not a separate read/check/write:
    // MutableStateFlow.compareAndSet only succeeds if the flow's value is
    // still exactly the `current` snapshot this loop just read, so a
    // concurrent set() landing between the read and the CAS attempt makes
    // the CAS fail (not silently overwrite the replacement) and this loop
    // re-reads and re-checks the fresh value instead of ever writing over
    // it. There is no window in which an unobserved concurrent
    // replacement can be clobbered.
    override fun invalidate(expectedSessionId: SessionId, expectedGeneration: Long): Boolean {
        while (true) {
            val current = _session.value
            if (current == null || current.id != expectedSessionId || current.generation != expectedGeneration) {
                return false
            }
            if (_session.compareAndSet(current, null)) {
                invalidated.add(expectedSessionId to expectedGeneration)
                return true
            }
            // `current` changed concurrently between the read above and the
            // CAS attempt -- retry against the fresh value rather than
            // treating the stale read as authoritative.
        }
    }

    fun set(value: AuthenticatedSession?) {
        _session.value = value
    }
}
