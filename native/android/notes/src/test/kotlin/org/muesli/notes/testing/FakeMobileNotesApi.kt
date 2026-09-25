package org.muesli.notes.testing

import kotlinx.coroutines.CompletableDeferred
import org.muesli.notes.api.ApiResult
import org.muesli.notes.api.AuthenticatedSession
import org.muesli.notes.api.MobileNotesApi
import org.muesli.notes.api.SessionId
import org.muesli.notes.model.NoteDetail
import org.muesli.notes.model.NotesPage

/**
 * A controllable [MobileNotesApi] double for repository/view-model tests
 * (issue #768 Task 3). Responses are queued per distinguishing key (list
 * cursor, or detail note id) and consumed FIFO; an optional per-key gate
 * lets a test suspend a call mid-flight to provoke a specific completion
 * order (e.g. a stale-generation race).
 */
class FakeMobileNotesApi : MobileNotesApi {
    data class ListCall(val sessionId: SessionId, val limit: Int, val cursor: String?)
    data class DetailCall(val sessionId: SessionId, val noteId: String)

    val listCalls = mutableListOf<ListCall>()
    val detailCalls = mutableListOf<DetailCall>()

    private val listResponses = mutableMapOf<String?, ArrayDeque<ApiResult<NotesPage>>>()
    private val detailResponses = mutableMapOf<String, ArrayDeque<ApiResult<NoteDetail>>>()
    private val listGates = mutableMapOf<String?, CompletableDeferred<Unit>>()

    fun enqueueList(cursor: String?, result: ApiResult<NotesPage>) {
        listResponses.getOrPut(cursor) { ArrayDeque() }.addLast(result)
    }

    fun enqueueDetail(noteId: String, result: ApiResult<NoteDetail>) {
        detailResponses.getOrPut(noteId) { ArrayDeque() }.addLast(result)
    }

    /** Returns a gate for [cursor]; complete it from the test to let a suspended call proceed. */
    fun gateList(cursor: String?): CompletableDeferred<Unit> =
        listGates.getOrPut(cursor) { CompletableDeferred() }

    override suspend fun listNotes(session: AuthenticatedSession, limit: Int, cursor: String?): ApiResult<NotesPage> {
        listCalls.add(ListCall(session.id, limit, cursor))
        // Claim this call's response synchronously (before any suspension) so
        // call order always matches enqueue order regardless of gate timing.
        val queue = listResponses[cursor] ?: error("no fake list response enqueued for cursor=$cursor")
        val response = queue.removeFirstOrNull() ?: error("fake list response queue exhausted for cursor=$cursor")
        listGates[cursor]?.await()
        return response
    }

    override suspend fun getNote(session: AuthenticatedSession, noteId: String): ApiResult<NoteDetail> {
        detailCalls.add(DetailCall(session.id, noteId))
        val queue = detailResponses[noteId] ?: error("no fake detail response enqueued for note=$noteId")
        return queue.removeFirstOrNull() ?: error("fake detail response queue exhausted for note=$noteId")
    }
}
