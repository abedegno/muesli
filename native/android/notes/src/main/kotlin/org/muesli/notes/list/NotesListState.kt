package org.muesli.notes.list

import org.muesli.notes.api.ApiFailure
import org.muesli.notes.model.NoteListItem

/** Forward ("next page") pagination status, mutually exclusive with [RefreshStatus.REFRESHING] and [PreviousPageStatus.LOADING_PREVIOUS_PAGE]. */
enum class PaginationStatus { IDLE, LOADING_NEXT_PAGE, NEXT_PAGE_FAILED }

/** Backward ("previous page" / evicted-window reload) status; shares one operation gate with pagination and refresh. */
enum class PreviousPageStatus { IDLE, LOADING_PREVIOUS_PAGE, PREVIOUS_PAGE_FAILED }

/** Refresh status; a refresh may only start while both [PaginationStatus] and [PreviousPageStatus] are non-busy. */
enum class RefreshStatus { IDLE, REFRESHING, REFRESH_FAILED }

/**
 * The notes collection screen's explicit, immutable state (issue #768). Only
 * [Content] can be displaying real rows; [InitialFailure] never claims the
 * collection is empty, and [Empty] is only reached on a genuinely empty
 * first page.
 */
sealed interface NotesListState {
    data object InitialLoading : NotesListState

    data class InitialFailure(val reason: ApiFailure) : NotesListState

    data object Empty : NotesListState

    data class Content(
        val rows: List<NoteListItem>,
        /** The stable id of the first visible row; moves only when the head page itself changes (i.e. was evicted or replaced), never on a plain tail append. */
        val anchorNoteId: String?,
        val canLoadNext: Boolean,
        val canLoadPrevious: Boolean,
        val paginationStatus: PaginationStatus,
        val previousPageStatus: PreviousPageStatus,
        val refreshStatus: RefreshStatus,
    ) : NotesListState
}
