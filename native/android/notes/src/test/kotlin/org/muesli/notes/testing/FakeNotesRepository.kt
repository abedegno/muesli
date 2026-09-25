package org.muesli.notes.testing

import kotlinx.coroutines.flow.MutableStateFlow
import org.muesli.notes.api.ApiResult
import org.muesli.notes.api.AuthenticatedSession
import org.muesli.notes.data.NotesRepository
import org.muesli.notes.list.NotesListState
import org.muesli.notes.model.NoteDetail

/** Records every call so [org.muesli.notes.list.NotesListViewModel] can be tested in isolation from repository internals. */
class FakeNotesRepository : NotesRepository {
    private val _state = MutableStateFlow<NotesListState>(NotesListState.InitialLoading)
    override val state = _state

    val loadFirstPageCalls = mutableListOf<AuthenticatedSession>()
    val loadNextPageCalls = mutableListOf<AuthenticatedSession>()
    val loadPreviousPageCalls = mutableListOf<AuthenticatedSession>()
    val refreshCalls = mutableListOf<AuthenticatedSession>()
    var clearCallCount = 0
        private set

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
        error("not used by NotesListViewModelTest")
    }

    override fun clear() {
        clearCallCount++
        _state.value = NotesListState.InitialLoading
    }

    fun publish(state: NotesListState) {
        _state.value = state
    }
}
