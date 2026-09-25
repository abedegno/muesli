package org.muesli.notes.detail

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import org.muesli.notes.api.ApiFailure
import org.muesli.notes.api.ApiResult
import org.muesli.notes.api.AuthenticatedSession
import org.muesli.notes.api.SessionId
import org.muesli.notes.data.NotesRepository
import org.muesli.notes.session.SessionSource

/**
 * Loads the authoritative detail for exactly one note, identified only by
 * its stable [noteId] -- never a possibly truncated list row (issue #768
 * Task 4). Every asynchronous result is verified against both the active
 * session and this view model's own request generation before publication,
 * so a late result from a disposed screen, a stale retry, or a superseded
 * session/account can never update visible state.
 */
class NoteDetailViewModel(
    private val noteId: String,
    private val sessionSource: SessionSource,
    private val repository: NotesRepository,
) : ViewModel() {

    private val _state = MutableStateFlow<NoteDetailState>(NoteDetailState.Loading)
    val state: StateFlow<NoteDetailState> = _state.asStateFlow()

    private var requestGeneration = 0L
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
            requestGeneration += 1 // discard any in-flight request tied to the former session
            _state.value = NoteDetailState.Loading
        } else {
            load(session)
        }
    }

    /** Retries the initial load or a transient failure. A no-op with no active session. */
    fun retry() {
        val session = sessionSource.session.value ?: return
        load(session)
    }

    private fun load(session: AuthenticatedSession) {
        requestGeneration += 1
        val generation = requestGeneration
        _state.value = NoteDetailState.Loading
        viewModelScope.launch {
            val result = repository.noteDetail(session, noteId)
            if (generation != requestGeneration || sessionSource.session.value?.id != session.id) {
                return@launch // superseded retry, disposed screen, or session/account change
            }
            _state.value = when (result) {
                is ApiResult.Success -> NoteDetailState.Content(result.value)
                is ApiResult.Failure -> when (val reason = result.reason) {
                    ApiFailure.Unavailable -> NoteDetailState.Unavailable
                    else -> NoteDetailState.Failure(reason)
                }
            }
        }
    }
}
