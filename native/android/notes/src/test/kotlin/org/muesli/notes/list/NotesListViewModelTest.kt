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
    fun `a same-id but newer-generation session (such as a token refresh) is treated as a transition and reloads`() = runTest {
        val sessionGen1 = FakeAuthenticatedSession("s1", generation = 1)
        val sessionGen2 = FakeAuthenticatedSession("s1", generation = 2)
        val sessionSource = FakeSessionSource(sessionGen1)
        val repository = FakeNotesRepository()
        NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(listOf(sessionGen1), repository.loadFirstPageCalls)

        sessionSource.set(sessionGen2)
        dispatcher.scheduler.advanceUntilIdle()
        assertEquals(
            "a bumped generation on the same session id must still trigger a fresh load",
            listOf(sessionGen1, sessionGen2),
            repository.loadFirstPageCalls,
        )
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

    @Test
    fun `signing out cancels the in-flight first-page load, not just discards its result`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val gate = repository.gateFirstPage()
        NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.runCurrent() // reach the gated await inside loadFirstPage

        sessionSource.set(null)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "sign-out must cancel the coroutine actually running the in-flight first-page load",
            1,
            repository.loadFirstPageCancelledCount,
        )
        assertEquals(1, repository.clearCallCount)
        gate.complete(Unit) // avoid leaking an uncompleted deferred past the test
    }

    @Test
    fun `switching accounts cancels the in-flight first-page load for the former account`() = runTest {
        val sessionA = FakeAuthenticatedSession("a")
        val sessionB = FakeAuthenticatedSession("b")
        val sessionSource = FakeSessionSource(sessionA)
        val repository = FakeNotesRepository()
        val gateA = repository.gateFirstPage()
        NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.runCurrent() // reach the gated await for account A

        sessionSource.set(sessionB)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "an account switch must cancel the former account's in-flight first-page load",
            1,
            repository.loadFirstPageCancelledCount,
        )
        assertEquals(listOf(sessionA, sessionB), repository.loadFirstPageCalls)
        gateA.complete(Unit)
    }

    @Test
    fun `signing out cancels an in-flight next-page load, not just discards its result`() = runTest {
        val session = FakeAuthenticatedSession("s1")
        val sessionSource = FakeSessionSource(session)
        val repository = FakeNotesRepository()
        val viewModel = NotesListViewModel(sessionSource, repository)
        dispatcher.scheduler.advanceUntilIdle() // let the initial (ungated) first-page load finish

        val gate = repository.gateNextPage()
        viewModel.loadNextPage()
        dispatcher.scheduler.runCurrent() // reach the gated await inside loadNextPage

        sessionSource.set(null)
        dispatcher.scheduler.advanceUntilIdle()

        assertEquals(
            "sign-out must cancel the coroutine actually running the in-flight next-page load",
            1,
            repository.loadNextPageCancelledCount,
        )
        gate.complete(Unit)
    }
}
