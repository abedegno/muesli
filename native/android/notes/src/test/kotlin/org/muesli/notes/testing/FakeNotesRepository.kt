package org.muesli.notes.testing

import kotlinx.coroutines.CancellationException
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
 *
 * Each operation can optionally be gated (via `gateXxx()`) so a test can
 * suspend it mid-flight, then prove real cancellation: if the coroutine
 * running the gated call is cancelled (e.g. because a view model cancels
 * its tracked job on a session/account/generation transition), the
 * `await()` below throws [CancellationException], which is recorded in the
 * matching `xxxCancelledCount` before being rethrown -- proof the
 * cancellation actually reached this suspended call, not just that a late
 * result was discarded.
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

    var loadFirstPageCancelledCount = 0
        private set
    var loadNextPageCancelledCount = 0
        private set
    var loadPreviousPageCancelledCount = 0
        private set
    var refreshCancelledCount = 0
        private set
    var noteDetailCancelledCount = 0
        private set

    private val detailResponses = mutableMapOf<String, ArrayDeque<ApiResult<NoteDetail>>>()
    private val detailCallGates = mutableMapOf<String, ArrayDeque<CompletableDeferred<Unit>>>()

    private var firstPageGate: CompletableDeferred<Unit>? = null
    private var nextPageGate: CompletableDeferred<Unit>? = null
    private var previousPageGate: CompletableDeferred<Unit>? = null
    private var refreshGate: CompletableDeferred<Unit>? = null

    fun enqueueDetail(noteId: String, result: ApiResult<NoteDetail>) {
        detailResponses.getOrPut(noteId) { ArrayDeque() }.addLast(result)
    }

    /** Gates only the *next* [noteDetail] call for [noteId]; later calls proceed immediately. */
    fun gateDetail(noteId: String): CompletableDeferred<Unit> {
        val g = CompletableDeferred<Unit>()
        detailCallGates.getOrPut(noteId) { ArrayDeque() }.addLast(g)
        return g
    }

    /** Gates the *next* [loadFirstPage] call. */
    fun gateFirstPage(): CompletableDeferred<Unit> = CompletableDeferred<Unit>().also { firstPageGate = it }

    /** Gates the *next* [loadNextPage] call. */
    fun gateNextPage(): CompletableDeferred<Unit> = CompletableDeferred<Unit>().also { nextPageGate = it }

    /** Gates the *next* [loadPreviousPage] call. */
    fun gatePreviousPage(): CompletableDeferred<Unit> = CompletableDeferred<Unit>().also { previousPageGate = it }

    /** Gates the *next* [refresh] call. */
    fun gateRefresh(): CompletableDeferred<Unit> = CompletableDeferred<Unit>().also { refreshGate = it }

    override suspend fun loadFirstPage(session: AuthenticatedSession) {
        loadFirstPageCalls.add(session)
        val gate = firstPageGate
        firstPageGate = null
        try {
            gate?.await()
        } catch (e: CancellationException) {
            loadFirstPageCancelledCount++
            throw e
        }
    }

    override suspend fun loadNextPage(session: AuthenticatedSession) {
        loadNextPageCalls.add(session)
        val gate = nextPageGate
        nextPageGate = null
        try {
            gate?.await()
        } catch (e: CancellationException) {
            loadNextPageCancelledCount++
            throw e
        }
    }

    override suspend fun loadPreviousPage(session: AuthenticatedSession) {
        loadPreviousPageCalls.add(session)
        val gate = previousPageGate
        previousPageGate = null
        try {
            gate?.await()
        } catch (e: CancellationException) {
            loadPreviousPageCancelledCount++
            throw e
        }
    }

    override suspend fun refresh(session: AuthenticatedSession) {
        refreshCalls.add(session)
        val gate = refreshGate
        refreshGate = null
        try {
            gate?.await()
        } catch (e: CancellationException) {
            refreshCancelledCount++
            throw e
        }
    }

    override suspend fun noteDetail(session: AuthenticatedSession, noteId: String): ApiResult<NoteDetail> {
        noteDetailCalls.add(session to noteId)
        // Claim this call's response synchronously (before any suspension)
        // so call order always matches enqueue order, even when a later
        // call's gate resolves before an earlier, still-suspended call's.
        val queue = detailResponses[noteId] ?: error("no fake detail response enqueued for note=$noteId")
        val response = queue.removeFirstOrNull() ?: error("fake detail response queue exhausted for note=$noteId")
        try {
            detailCallGates[noteId]?.removeFirstOrNull()?.await()
        } catch (e: CancellationException) {
            noteDetailCancelledCount++
            throw e
        }
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
