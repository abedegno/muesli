package org.muesli.notes.ui

import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.muesli.notes.list.NotesListState
import org.muesli.notes.list.PaginationStatus
import org.muesli.notes.list.PreviousPageStatus
import org.muesli.notes.list.RefreshStatus
import org.muesli.notes.testing.testNoteItem

/**
 * Compose UI tests for [NotesListScreen] (issue #768 Task 5): every
 * collection state, retained rows under a pagination/refresh error, inert
 * refresh/edge controls during another in-flight operation, and the absence
 * of any mutation/audio/search affordance.
 *
 * Requires a device/emulator (`:notes:connectedDebugAndroidTest`); this
 * sandbox has no `/dev/kvm` so these were compile-checked
 * (`:notes:compileDebugAndroidTestKotlin`) but could not be executed here.
 */
@RunWith(AndroidJUnit4::class)
class NotesListScreenTest {
    @get:Rule
    val compose = createComposeRule()

    private fun content(
        rows: List<org.muesli.notes.model.NoteListItem> = listOf(testNoteItem("a")),
        canLoadNext: Boolean = false,
        canLoadPrevious: Boolean = false,
        paginationStatus: PaginationStatus = PaginationStatus.IDLE,
        previousPageStatus: PreviousPageStatus = PreviousPageStatus.IDLE,
        refreshStatus: RefreshStatus = RefreshStatus.IDLE,
    ) = NotesListState.Content(
        rows = rows,
        anchorNoteId = rows.firstOrNull()?.id,
        canLoadNext = canLoadNext,
        canLoadPrevious = canLoadPrevious,
        paginationStatus = paginationStatus,
        previousPageStatus = previousPageStatus,
        refreshStatus = refreshStatus,
    )

    @Test
    fun initialLoadingShowsLoadingMessage() {
        compose.setContent {
            NotesListScreen(NotesListState.InitialLoading, {}, {}, {}, {}, {})
        }
        compose.onNodeWithText("Loading notes…").assertExists()
    }

    @Test
    fun emptyShowsEmptyMessage() {
        compose.setContent {
            NotesListScreen(NotesListState.Empty, {}, {}, {}, {}, {})
        }
        compose.onNodeWithText("No notes yet.").assertExists()
    }

    @Test
    fun initialFailureOffersRetryAndNeverClaimsEmpty() {
        compose.setContent {
            NotesListScreen(NotesListState.InitialFailure(org.muesli.notes.api.ApiFailure.Http(500)), {}, {}, {}, {}, {})
        }
        compose.onNodeWithText("Couldn't load your notes.").assertExists()
        compose.onNodeWithText("No notes yet.").assertDoesNotExist()
        compose.onNodeWithText("Retry").assertExists().assertIsEnabled()
    }

    @Test
    fun populatedContentShowsRows() {
        val rows = listOf(testNoteItem("a"), testNoteItem("b"))
        compose.setContent {
            NotesListScreen(content(rows = rows), {}, {}, {}, {}, {})
        }
        compose.onNodeWithText("Note a").assertExists()
        compose.onNodeWithText("Note b").assertExists()
    }

    @Test
    fun paginationErrorPreservesRowsAndOffersRetryForThatEdgeOnly() {
        val rows = listOf(testNoteItem("a"))
        compose.setContent {
            NotesListScreen(
                content(rows = rows, canLoadNext = true, paginationStatus = PaginationStatus.NEXT_PAGE_FAILED),
                {}, {}, {}, {}, {},
            )
        }
        compose.onNodeWithText("Note a").assertExists()
        compose.onNodeWithText("Couldn't load more notes.").assertExists()
    }

    @Test
    fun refreshErrorKeepsRowsVisible() {
        val rows = listOf(testNoteItem("a"))
        compose.setContent {
            NotesListScreen(content(rows = rows, refreshStatus = RefreshStatus.REFRESH_FAILED), {}, {}, {}, {}, {})
        }
        compose.onNodeWithText("Note a").assertExists()
        compose.onNodeWithText("Refresh failed.").assertExists()
    }

    @Test
    fun refreshControlIsInertWhilePaginationInFlight() {
        compose.setContent {
            NotesListScreen(
                content(canLoadNext = true, paginationStatus = PaginationStatus.LOADING_NEXT_PAGE),
                {}, {}, {}, {}, {},
            )
        }
        compose.onNodeWithText("Refresh").assertIsNotEnabled()
    }

    @Test
    fun nextPageControlIsInertWhileRefreshInFlight() {
        compose.setContent {
            NotesListScreen(
                content(canLoadNext = true, refreshStatus = RefreshStatus.REFRESHING),
                {}, {}, {}, {}, {},
            )
        }
        compose.onNodeWithText("Load more notes").assertIsNotEnabled()
    }

    @Test
    fun selectingARowNavigatesByIdOnly() {
        var selected: String? = null
        compose.setContent {
            NotesListScreen(content(rows = listOf(testNoteItem("row-1"))), { id -> selected = id }, {}, {}, {}, {})
        }
        compose.onNodeWithText("Note row-1").performClick()
        assert(selected == "row-1")
    }

    @Test
    fun noMutationOrAudioOrSearchAffordanceIsRendered() {
        compose.setContent {
            NotesListScreen(content(rows = listOf(testNoteItem("a"))), {}, {}, {}, {}, {})
        }
        for (forbidden in listOf("Delete", "Edit", "Record", "Upload", "Share", "Search", "New note", "Create")) {
            compose.onNodeWithText(forbidden).assertDoesNotExist()
        }
    }
}
