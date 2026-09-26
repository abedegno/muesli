package org.muesli.notes.ui

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.muesli.notes.list.NotesListViewModel
import org.muesli.notes.testing.FakeAuthenticatedSession
import org.muesli.notes.testing.FakeNotesRepository
import org.muesli.notes.testing.FakeSessionSource

/**
 * Lifecycle tests for the notes feature's view-model wiring (issue #768 Task
 * 5, unit-test half of the Task 5 coverage list -- the Compose recreation
 * scenario itself is exercised by the (uninstrumentable-here) androidTest
 * suite). Confirms that recreating the *screen* around a *surviving*
 * ViewModel instance -- the real Android guarantee `NotesFeature` relies on
 * -- never re-triggers a fetch, and that removing screen A's session/state
 * happens before screen B's request is ever admitted.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class NotesFeatureLifecycleTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
    }

    @After
    fun tearDown() {
        Dispatchers.resetMain()
    }

    @Test
    fun `re-subscribing to a surviving view model's state does not re-trigger a fetch`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(1, repository.loadFirstPageCalls.size)

        // Simulate a configuration change: the screen recomposes and
        // re-collects `viewModel.state`, but the ViewModel instance itself
        // (per the AndroidX ViewModelStore guarantee `NotesFeature` relies
        // on) is the same one, not a fresh one.
        repeat(3) {
            val collected = viewModel.state.value
            assertEquals(repository.state.value, collected)
        }
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals("recreation must not duplicate the initial fetch", 1, repository.loadFirstPageCalls.size)
    }

    @Test
    fun `switching accounts clears the former session before the new session's load is admitted`() = runTest {
        val sessionA = FakeAuthenticatedSession("a")
        val sessionB = FakeAuthenticatedSession("b")
        val sessionSource = FakeSessionSource(sessionA)
        val repository = FakeNotesRepository()
        NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(listOf(sessionA), repository.loadFirstPageCalls)
        assertEquals(0, repository.clearCallCount)

        sessionSource.set(null) // e.g. sign-out as part of an account switch
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(1, repository.clearCallCount)

        sessionSource.set(sessionB)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(
            "B's load must be issued only after A's state was cleared",
            listOf(sessionA, sessionB),
            repository.loadFirstPageCalls,
        )
    }
}
