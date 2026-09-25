package org.muesli.notes.testing

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.MutableStateFlow
import org.muesli.notes.api.ApiResult
import org.muesli.notes.api.AuthenticatedSession
import org.muesli.notes.data.NotesRepository
import org.muesli.notes.list.NotesListState
import org.muesli.notes.model.NoteDetail

/**
 * Records every call so [org.muesli.notes.list.NotesListViewModel] and
 * [org.muesli.notes.detail.NoteDetailViewModel] can be tested in isolation
 * from repository internals (covered separately by
 * SessionNotesRepositoryTest).
 */
class FakeNotesRepository : NotesRepository {
    private val _state = MutableStateFlow<NotesListState>(NotesListState.InitialLoading)
    override val state = _state

    val loadFirstPageCalls = mutableListOf<AuthenticatedSession>()
    val loadNextPageCalls = mutableListOf<AuthenticatedSession>()
    val loadPreviousPageCalls = mutableListOf<AuthenticatedSession>()
    val refreshCalls = mutableListOf<AuthenticatedSession>()
    val noteDetailCalls = mutableListOf<Pair<AuthenticatedSession, String>>()
    var clearCallCount = 0
        private set

    private val detailResponses = mutableMapOf<String, ArrayDeque<ApiResult<NoteDetail>>>()
    private val detailCallGates = mutableMapOf<String, ArrayDeque<CompletableDeferred<Unit>>>()

    fun enqueueDetail(noteId: String, result: ApiResult<NoteDetail>) {
        detailResponses.getOrPut(noteId) { ArrayDeque() }.addLast(result)
    }

    /** Gates only the *next* [noteDetail] call for [noteId]; later calls proceed immediately. */
    fun gateDetail(noteId: String): CompletableDeferred<Unit> {
        val g = CompletableDeferred<Unit>()
        detailCallGates.getOrPut(noteId) { ArrayDeque() }.addLast(g)
        return g
    }

    override suspend fun loadFirstPage(session: AuthenticatedSession) {
        loadFirstPageCalls.add(session)
    }

    override suspend fun loadNextPage(session: AuthenticatedSession) {
        loadNextPageCalls.add(session)
    }

    override suspend fun loadPreviousPage(session: AuthenticatedSession) {
        loadPreviousPageCalls.add(session)
    }

    override suspend fun refresh(session: AuthenticatedSession) {
        refreshCalls.add(session)
    }

    override suspend fun noteDetail(session: AuthenticatedSession, noteId: String): ApiResult<NoteDetail> {
        noteDetailCalls.add(session to noteId)
        // Claim this call's response synchronously (before any suspension)
        // so call order always matches enqueue order, even when a later
        // call's gate resolves before an earlier, still-suspended call's.
        val queue = detailResponses[noteId] ?: error("no fake detail response enqueued for note=$noteId")
        val response = queue.removeFirstOrNull() ?: error("fake detail response queue exhausted for note=$noteId")
        detailCallGates[noteId]?.removeFirstOrNull()?.await()
        return response
    }

    override fun clear() {
        clearCallCount++
        _state.value = NotesListState.InitialLoading
    }

    fun publish(state: NotesListState) {
        _state.value = state
    }
}
