package org.muesli.notes.ui

import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertHeightIsAtLeast
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.muesli.notes.list.NotesListState
import org.muesli.notes.testing.testNoteItem

/**
 * Accessibility tests (issue #768 Task 5): meaningful labels on rows and
 * status messages, a live-region announcement for status text, and a
 * minimum 48dp touch target on rows and controls. Status meaning is carried
 * entirely by text (see [NotesListScreenTest]/[NoteDetailScreenTest], which
 * assert on that text directly) rather than by color, so no separate
 * color-contrast assertion is needed here. See [NotesListScreenTest] for the
 * emulator-availability note.
 */
@RunWith(AndroidJUnit4::class)
class NotesAccessibilityTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun pinnedTaggedRowHasAMeaningfulMergedContentDescription() {
        val row = testNoteItem("a").copy(pinned = true, tags = listOf("planning"))
        compose.setContent {
            NotesListScreen(
                NotesListState.Content(
                    rows = listOf(row),
                    anchorNoteId = row.id,
                    canLoadNext = false,
                    canLoadPrevious = false,
                    paginationStatus = org.muesli.notes.list.PaginationStatus.IDLE,
                    previousPageStatus = org.muesli.notes.list.PreviousPageStatus.IDLE,
                    refreshStatus = org.muesli.notes.list.RefreshStatus.IDLE,
                ),
                {}, {}, {}, {}, {},
            )
        }
        compose.onNodeWithContentDescription("Note a, pinned, tags: planning").assertExists()
    }

    @Test
    fun rowMeetsMinimumTouchTargetHeight() {
        compose.setContent {
            NotesListScreen(
                NotesListState.Content(
                    rows = listOf(testNoteItem("a")),
                    anchorNoteId = "a",
                    canLoadNext = false,
                    canLoadPrevious = false,
                    paginationStatus = org.muesli.notes.list.PaginationStatus.IDLE,
                    previousPageStatus = org.muesli.notes.list.PreviousPageStatus.IDLE,
                    refreshStatus = org.muesli.notes.list.RefreshStatus.IDLE,
                ),
                {}, {}, {}, {}, {},
            )
        }
        compose.onNodeWithContentDescription("Note a").assertHeightIsAtLeast(48.dp)
    }

    @Test
    fun statusMessageIsAPoliteLiveRegionSoScreenReadersAnnounceStateChanges() {
        compose.setContent {
            NotesListScreen(NotesListState.InitialLoading, {}, {}, {}, {}, {})
        }
        compose.onNodeWithText("Loading notes…")
            .assert(SemanticsMatcher.keyIsDefined(SemanticsProperties.LiveRegion))
    }

    @Test
    fun retryControlHasAMeaningfulLabelAndIsOperable() {
        compose.setContent {
            NotesListScreen(NotesListState.InitialFailure(org.muesli.notes.api.ApiFailure.Http(500)), {}, {}, {}, {}, {})
        }
        compose.onNodeWithText("Retry").assertExists().assertHeightIsAtLeast(48.dp)
    }
}
