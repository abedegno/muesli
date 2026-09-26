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

    // Tracks (id, generation) rather than just id: a host can replace the
    // AuthenticatedSession (e.g. on token refresh / re-auth) while keeping
    // the same id, and that must still be treated as a session transition.
    private var observedSessionKey: Pair<SessionId, Long>? = null

    // The single in-flight repository call (first page, next page, previous
    // page, or refresh) this view model itself launched, if any. Cancelled
    // on every session/account/generation transition so a superseded
    // request's underlying network call is actually torn down, not merely
    // ignored once it eventually completes.
    private var currentJob: Job? = null

    init {
        viewModelScope.launch {
            sessionSource.session.collect { session -> onSessionChanged(session) }
        }
    }

    private fun onSessionChanged(session: AuthenticatedSession?) {
        val newKey = session?.let { it.id to it.generation }
        if (newKey == observedSessionKey) return
        observedSessionKey = newKey
        currentJob?.cancel()
        if (session == null) {
            repository.clear()
            currentJob = null
        } else {
            currentJob = viewModelScope.launch { repository.loadFirstPage(session) }
        }
    }

    fun retryInitialLoad(): Job? = launchWithSession { repository.loadFirstPage(it) }
    fun loadNextPage(): Job? = launchWithSession { repository.loadNextPage(it) }
    fun loadPreviousPage(): Job? = launchWithSession { repository.loadPreviousPage(it) }
    fun refresh(): Job? = launchWithSession { repository.refresh(it) }

    private fun launchWithSession(block: suspend (AuthenticatedSession) -> Unit): Job? {
        val session = sessionSource.session.value ?: return null
        val job = viewModelScope.launch { block(session) }
        currentJob = job
        return job
    }
}
