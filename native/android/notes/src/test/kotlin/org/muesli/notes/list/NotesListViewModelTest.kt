package org.muesli.notes.list

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
import org.muesli.notes.testing.FakeAuthenticatedSession
import org.muesli.notes.testing.FakeNotesRepository
import org.muesli.notes.testing.FakeSessionSource

/**
 * View-model tests (issue #768 Task 3): session-change reactions (load on
 * sign-in/account switch, clear on sign-out) and thin forwarding of user
 * intents to the repository, using a recording [FakeNotesRepository] so
 * these tests are independent of the repository's own windowing/gating
 * logic (covered by SessionNotesRepositoryTest).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class NotesListViewModelTest {
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
    fun `signing in loads the first page for the new session`() = runTest {
        val sessionSource = FakeSessionSource()
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        val session = FakeAuthenticatedSession("s1")

        sessionSource.set(session)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(listOf(session), repository.loadFirstPageCalls)
    }

    @Test
    fun `signing out clears repository state`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(listOf(session), repository.loadFirstPageCalls)

        sessionSource.set(null)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(1, repository.clearCallCount)
    }

    @Test
    fun `switching accounts reloads exactly once per account, no duplicate fetch on unrelated recomposition`() = runTest {
        val sessionA = FakeAuthenticatedSession("a")
        val sessionB = FakeAuthenticatedSession("b")
        val sessionSource = FakeSessionSource(sessionA)
        val repository = FakeNotesRepository()
        NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()

        // Re-emitting the same session (e.g. a recreation re-collecting the
        // flow) must not trigger a second fetch.
        sessionSource.set(sessionA)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(listOf(sessionA), repository.loadFirstPageCalls)

        sessionSource.set(sessionB)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(listOf(sessionA, sessionB), repository.loadFirstPageCalls)
    }

    @Test
    fun `loadNextPage, loadPreviousPage, refresh, and retry forward to the repository with the current session`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()

        viewModel.loadNextPage()
        viewModel.loadPreviousPage()
        viewModel.refresh()
        viewModel.retryInitialLoad()
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(listOf(session), repository.loadNextPageCalls)
        assertEquals(listOf(session), repository.loadPreviousPageCalls)
        assertEquals(listOf(session), repository.refreshCalls)
        assertEquals(listOf(session, session), repository.loadFirstPageCalls) // sign-in + retry
    }

    @Test
    fun `user intents are a no-op with no active session`() = runTest {
        val sessionSource = FakeSessionSource(null)
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()

        viewModel.loadNextPage()
        viewModel.loadPreviousPage()
        viewModel.refresh()
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(emptyList<Any>(), repository.loadNextPageCalls)
        assertEquals(emptyList<Any>(), repository.loadPreviousPageCalls)
        assertEquals(emptyList<Any>(), repository.refreshCalls)
    }

    @Test
    fun `exposed state is the repository's published state`() = runTest {
        val sessionSource = FakeSessionSource()
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        repository.publish(NotesListState.Empty)
        assertEquals(NotesListState.Empty, viewModel.state.value)
    }
}
