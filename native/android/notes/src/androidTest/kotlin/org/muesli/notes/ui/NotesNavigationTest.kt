package org.muesli.notes.ui

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.navigation.compose.rememberNavController
import androidx.navigation.testing.TestNavHostController
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.muesli.notes.api.ApiResult
import org.muesli.notes.testing.FakeAuthenticatedSession
import org.muesli.notes.testing.FakeMobileNotesApi
import org.muesli.notes.testing.FakeSessionSource
import org.muesli.notes.testing.testPage

/**
 * Compose navigation tests (issue #768 Task 5): authenticated entry into the
 * notes destination, id-only selection into detail, and back navigation. See
 * [NotesListScreenTest] for the emulator-availability note.
 */
@RunWith(AndroidJUnit4::class)
class NotesNavigationTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun selectingARowNavigatesToDetailByIdOnlyAndBackReturnsToTheList() {
        val api = FakeMobileNotesApi()
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        api.enqueueList(null, ApiResult.Success(testPage("row", 1, nextCursor = null)))
        api.enqueueDetail("row-0", ApiResult.Success(org.muesli.notes.testing.testDetail("row-0")))
        val repository = org.muesli.notes.data.SessionNotesRepository(api, sessionSource)

        lateinit var navController: TestNavHostController
        compose.setContent {
            navController = TestNavHostController(InstrumentationRegistry.getInstrumentation().targetContext)
            navController.navigatorProvider.addNavigator(androidx.navigation.compose.ComposeNavigator())
            NotesNavGraph(navController = navController, sessionSource = sessionSource, repository = repository)
        }

        compose.waitUntil(timeoutMillis = 5_000) {
            compose.onAllNodesWithText("Note row-0").fetchSemanticsNodes().isNotEmpty()
        }
        compose.onNodeWithText("Note row-0").performClick()

        compose.waitUntil(timeoutMillis = 5_000) {
            navController.currentBackStackEntry?.destination?.route == NOTES_DETAIL_ROUTE
        }
        assertEquals("row-0", navController.currentBackStackEntry?.arguments?.getString(NOTES_DETAIL_ARG_NOTE_ID))
    }
}
