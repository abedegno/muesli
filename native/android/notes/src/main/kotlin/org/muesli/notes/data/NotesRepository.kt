package org.muesli.notes.data

import kotlinx.coroutines.flow.StateFlow
import org.muesli.notes.api.ApiResult
import org.muesli.notes.api.AuthenticatedSession
import org.muesli.notes.list.NotesListState
import org.muesli.notes.model.NoteDetail

/**
 * The session-scoped coordinator between the #767 [org.muesli.notes.api.MobileNotesApi]
 * and the notes collection screen (issue #768). Owns the bounded page window,
 * de-duplication, the shared pagination/previous-page/refresh operation
 * gate, and session/generation-based rejection of stale results. The single
 * production implementation is [SessionNotesRepository].
 */
interface NotesRepository {
    val state: StateFlow<NotesListState>

    /** Starts (or restarts) the collection chain for [session]: always resets the window and bumps the collection generation. */
    suspend fun loadFirstPage(session: AuthenticatedSession)

    /** Ignored (no-op) unless [state] is [NotesListState.Content] with pagination idle/failed and no other operation in flight. */
    suspend fun loadNextPage(session: AuthenticatedSession)

    /** Ignored (no-op) unless a previous-page edge key is available and no other operation is in flight. */
    suspend fun loadPreviousPage(session: AuthenticatedSession)

    /** Ignored (no-op) unless pagination/previous-page are idle/failed and refresh is not already in flight. */
    suspend fun refresh(session: AuthenticatedSession)

    /** A stateless, non-cached authoritative detail fetch; does not touch collection state. */
    suspend fun noteDetail(session: AuthenticatedSession, noteId: String): ApiResult<NoteDetail>

    /** Clears all session-owned state (sign-out / account change) and returns [state] to [NotesListState.InitialLoading]. */
    fun clear()
}
