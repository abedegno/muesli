package org.muesli.notes.list

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import org.muesli.notes.api.AuthenticatedSession
import org.muesli.notes.api.SessionId
import org.muesli.notes.data.NotesRepository
import org.muesli.notes.session.SessionSource

/**
 * Thin lifecycle wrapper around [NotesRepository] (issue #768 Task 3): all
 * gating, de-duplication, windowing, and stale-result rejection lives in the
 * repository so it is testable without any UI or lifecycle machinery. This
 * class only forwards user intents, reacts to session/account transitions,
 * and exposes the repository's published [NotesListState]. `onCleared`
 * needs no manual cleanup: [viewModelScope] cancellation propagates through
 * every suspend call this class launches, down to the underlying HTTP call.
 */
class NotesListViewModel(
    private val sessionSource: SessionSource,
    private val repository: NotesRepository,
) : ViewModel() {

    val state: StateFlow<NotesListState> = repository.state

    private var observedSessionId: SessionId? = null

    init {
        viewModelScope.launch {
            sessionSource.session.collect { session -> onSessionChanged(session) }
        }
    }

    private fun onSessionChanged(session: AuthenticatedSession?) {
        val newId = session?.id
        if (newId == observedSessionId) return
        observedSessionId = newId
        if (session == null) {
            repository.clear()
        } else {
            viewModelScope.launch { repository.loadFirstPage(session) }
        }
    }

    fun retryInitialLoad(): Job? = launchWithSession { repository.loadFirstPage(it) }
    fun loadNextPage(): Job? = launchWithSession { repository.loadNextPage(it) }
    fun loadPreviousPage(): Job? = launchWithSession { repository.loadPreviousPage(it) }
    fun refresh(): Job? = launchWithSession { repository.refresh(it) }

    private fun launchWithSession(block: suspend (AuthenticatedSession) -> Unit): Job? {
        val session = sessionSource.session.value ?: return null
        return viewModelScope.launch { block(session) }
    }
}
