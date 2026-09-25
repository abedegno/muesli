package org.muesli.notes.detail

import org.muesli.notes.api.ApiFailure
import org.muesli.notes.model.NoteDetail

/**
 * The read-only note detail screen's explicit state (issue #768 Task 4).
 * [Unavailable] covers the contract's not-found/no-longer-accessible result
 * (offers navigation back, never a retry); any other failure is transient
 * and offers retry.
 */
sealed interface NoteDetailState {
    data object Loading : NoteDetailState
    data class Content(val detail: NoteDetail) : NoteDetailState
    data object Unavailable : NoteDetailState
    data class Failure(val reason: ApiFailure) : NoteDetailState
}
