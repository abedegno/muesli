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

    override fun invalidate(expectedSessionId: SessionId, expectedGeneration: Long) {
        val current = _session.value
        if (current != null && current.id == expectedSessionId && current.generation == expectedGeneration) {
            invalidated.add(expectedSessionId to expectedGeneration)
            _session.value = null
        }
    }

    fun set(value: AuthenticatedSession?) {
        _session.value = value
    }
}
