package org.muesli.notes.ui

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.muesli.notes.api.ApiFailure
import org.muesli.notes.detail.NoteDetailState
import org.muesli.notes.model.NoteDetail
import java.time.Instant

/**
 * Compose UI tests for [NoteDetailScreen] (issue #768 Task 5): loading,
 * complete/optional detail rendering, unavailable (back only), and failure
 * (retry). See [NotesListScreenTest] for the emulator-availability note.
 */
@RunWith(AndroidJUnit4::class)
class NoteDetailScreenTest {
    @get:Rule
    val compose = createComposeRule()

    private fun completeDetail() = NoteDetail(
        id = "n1",
        title = "Sprint planning",
        status = "ready",
        pinned = true,
        startedAt = Instant.parse("2026-01-10T09:00:00Z"),
        endedAt = Instant.parse("2026-01-10T09:45:00Z"),
        createdAt = Instant.parse("2026-01-10T09:00:05Z"),
        updatedAt = Instant.parse("2026-01-10T09:45:10Z"),
        tags = listOf("planning", "roadmap"),
        bodyMarkdown = "Discussed roadmap for Q1.",
        summaries = emptyList(),
    )

    private fun optionalAbsentDetail() = NoteDetail(
        id = "n2",
        title = "Oldest note",
        status = "processing",
        pinned = false,
        startedAt = null,
        endedAt = null,
        createdAt = Instant.parse("2026-01-01T08:00:00Z"),
        updatedAt = Instant.parse("2026-01-01T08:00:00Z"),
        tags = emptyList(),
        bodyMarkdown = "",
        summaries = emptyList(),
    )

    @Test
    fun loadingShowsLoadingMessage() {
        compose.setContent { NoteDetailScreen(NoteDetailState.Loading, {}, {}) }
        compose.onNodeWithText("Loading note…").assertExists()
    }

    @Test
    fun completeDetailRendersTitleTagsAndBody() {
        compose.setContent { NoteDetailScreen(NoteDetailState.Content(completeDetail()), {}, {}) }
        compose.onNodeWithText("Sprint planning").assertExists()
        compose.onNodeWithText("planning, roadmap").assertExists()
        compose.onNodeWithText("Discussed roadmap for Q1.").assertExists()
    }

    @Test
    fun optionalAbsentDetailOmitsTagsAndBodyWithoutFabricatingContent() {
        compose.setContent { NoteDetailScreen(NoteDetailState.Content(optionalAbsentDetail()), {}, {}) }
        compose.onNodeWithText("Oldest note").assertExists()
        compose.onNodeWithText("planning, roadmap").assertDoesNotExist()
    }

    @Test
    fun unavailableOffersOnlyBack() {
        var backClicked = false
        compose.setContent { NoteDetailScreen(NoteDetailState.Unavailable, { backClicked = true }, {}) }
        compose.onNodeWithText("This note is no longer available.").assertExists()
        compose.onNodeWithText("Back").performClick()
        assert(backClicked)
        compose.onNodeWithText("Retry").assertDoesNotExist()
    }

    @Test
    fun failureOffersRetry() {
        var retried = false
        compose.setContent { NoteDetailScreen(NoteDetailState.Failure(ApiFailure.Http(503)), {}, { retried = true }) }
        compose.onNodeWithText("Couldn't load this note.").assertExists()
        compose.onNodeWithText("Retry").performClick()
        assert(retried)
    }

    @Test
    fun noMutationOrAudioAffordanceIsRendered() {
        compose.setContent { NoteDetailScreen(NoteDetailState.Content(completeDetail()), {}, {}) }
        for (forbidden in listOf("Delete", "Edit", "Record", "Upload", "Share")) {
            compose.onNodeWithText(forbidden).assertDoesNotExist()
        }
    }
}
