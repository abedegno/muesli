package org.muesli.notes.api

import org.muesli.notes.model.NoteDetail
import org.muesli.notes.model.NotesPage

/**
 * The #767 mobile notes contract boundary. The single production
 * implementation is [OkHttpMobileNotesApi]; tests substitute a fake. An
 * implementation must not persist anything across calls, must not retry
 * internally, and must let [kotlinx.coroutines.CancellationException]
 * propagate rather than mapping it to an [ApiFailure].
 */
interface MobileNotesApi {
    /** GET /api/mobile/v1/notes. [limit] must be 1..100 (contract-validated by the caller). */
    suspend fun listNotes(session: AuthenticatedSession, limit: Int, cursor: String?): ApiResult<NotesPage>

    /** GET /api/mobile/v1/notes/{id}: the authoritative, complete detail for one note. */
    suspend fun getNote(session: AuthenticatedSession, noteId: String): ApiResult<NoteDetail>
}

/**
 * Everything a [MobileNotesApi] call needs from the host's authenticated
 * session, without exposing raw credentials to callers: a non-loggable
 * [id] for equality/logging, a monotonically increasing [generation] the
 * repository uses to reject stale results across a sign-out or account
 * change, the mobile API [baseUrl], and freshly computed [headers] the host
 * recomputes (never caches) on every call.
 */
interface AuthenticatedSession {
    val id: SessionId
    val generation: Long
    val baseUrl: String

    /** Recomputed on every call by the host; must never be cached by an [MobileNotesApi] implementation. */
    fun headers(): Map<String, String>
}

/**
 * An opaque session identity. [toString] is deliberately masked so a
 * session accidentally reaching a log statement never leaks anything
 * identifying; use [value] only where a raw comparison is required.
 */
@JvmInline
value class SessionId(val value: String) {
    override fun toString(): String = "SessionId(***)"
}
